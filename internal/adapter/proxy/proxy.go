package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/classify"
	crabwiseOtel "github.com/crabwise-ai/crabwise/internal/otel"
)

type Evaluator interface {
	Evaluate(e *audit.AuditEvent) audit.EvalResult
}

type Metrics struct {
	ActiveConnections atomic.Int64
	TotalRequests     atomic.Uint64
	TotalBlocked      atomic.Uint64
	UpstreamErrors    atomic.Uint64
	MappingDegraded   atomic.Uint64
}

type Proxy struct {
	cfg        Config
	evaluator  Evaluator
	classifier classify.Classifier
	events     chan<- *audit.AuditEvent
	attributor RequestAttributor

	router         *Router
	providers      map[string]*ProviderRuntime
	providersMu    sync.RWMutex
	certCache      *CertCache
	httpSrv        *http.Server
	httpSrvMu      sync.Mutex
	metrics        Metrics
	rawPayloads    RawPayloadWriter
	extraRedactREs []*regexp.Regexp
}

func (p *Proxy) SetEvaluator(e Evaluator) {
	p.evaluator = e
}

func (p *Proxy) SetClassifier(c classify.Classifier) {
	if c != nil {
		p.classifier = c
	}
}

func (p *Proxy) SetRequestAttributor(a RequestAttributor) {
	p.attributor = a
}

func (p *Proxy) HasRequestAttributor() bool {
	return p.attributor != nil
}

func New(cfg Config, evaluator Evaluator, classifier classify.Classifier, events chan<- *audit.AuditEvent) (*Proxy, error) {
	if classifier == nil {
		classifier = classify.NewFallbackRegistry()
	}

	providers, err := buildProviders(cfg)
	if err != nil {
		return nil, err
	}

	router, err := NewRouter(cfg.DefaultProvider, providers)
	if err != nil {
		return nil, err
	}

	extraREs, err := CompilePatterns(cfg.RedactPatterns)
	if err != nil {
		return nil, fmt.Errorf("proxy config: %w", err)
	}

	p := &Proxy{
		cfg:            cfg,
		evaluator:      evaluator,
		classifier:     classifier,
		events:         events,
		router:         router,
		providers:      providers,
		extraRedactREs: extraREs,
	}

	if cfg.CACert != "" && cfg.CAKey != "" {
		ca, err := LoadCA(cfg.CACert, cfg.CAKey)
		if err != nil {
			return nil, fmt.Errorf("load CA for MITM proxy: %w", err)
		}
		p.certCache = NewCertCache(ca, 256)
	}

	return p, nil
}

func buildProviders(cfg Config) (map[string]*ProviderRuntime, error) {
	providers := make(map[string]*ProviderRuntime, len(cfg.Providers))

	for name, providerCfg := range cfg.Providers {
		pc := providerCfg
		pc.Name = name

		factory, ok := lookupTransportFactory(name)
		if !ok {
			return nil, fmt.Errorf("no transport registered for provider %q (registered transports: %s)", name, registeredTransportNames())
		}
		transport := factory(pc, cfg.UpstreamTimeout)

		spec, err := LoadProviderSpec(cfg.MappingsDir, name, embeddedFallback(name))
		if err != nil {
			return nil, fmt.Errorf("load provider mapping %s: %w", name, err)
		}

		providers[name] = &ProviderRuntime{
			Name:      name,
			Config:    pc,
			Transport: transport,
			Mapping:   spec,
		}
	}
	return providers, nil
}

func embeddedFallback(name string) []byte {
	switch strings.ToLower(name) {
	case "openai":
		return embeddedOpenAIMapping
	default:
		return nil
	}
}

// embeddedOpenAIMapping is set by configs package init.
var embeddedOpenAIMapping []byte

func SetEmbeddedOpenAIMapping(data []byte) {
	embeddedOpenAIMapping = data
}

func registeredTransportNames() string {
	transportsMu.RLock()
	defer transportsMu.RUnlock()
	names := make([]string, 0, len(transportFactories))
	for n := range transportFactories {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func (p *Proxy) SetRawPayloadWriter(w RawPayloadWriter) {
	p.rawPayloads = w
}

// ReloadMappings reloads provider mapping specs from disk with atomic swap.
func (p *Proxy) ReloadMappings() error {
	newProviders, err := buildProviders(p.cfg)
	if err != nil {
		return fmt.Errorf("reload mappings: %w", err)
	}
	newRouter, err := NewRouter(p.cfg.DefaultProvider, newProviders)
	if err != nil {
		return fmt.Errorf("reload router: %w", err)
	}

	p.providersMu.Lock()
	p.providers = newProviders
	p.router = newRouter
	p.providersMu.Unlock()
	return nil
}

func (p *Proxy) Start(ctx context.Context) error {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			p.handleConnect(w, r)
			return
		}
		if r.URL.Path == "/health" {
			p.handleHealth(w, r)
			return
		}
		p.handleProxy(w, r)
	})

	srv := &http.Server{
		Addr:    p.cfg.Listen,
		Handler: handler,
	}

	p.httpSrvMu.Lock()
	p.httpSrv = srv
	p.httpSrvMu.Unlock()

	go func() {
		<-ctx.Done()
		_ = p.Stop()
	}()

	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (p *Proxy) Stop() error {
	p.httpSrvMu.Lock()
	srv := p.httpSrv
	p.httpSrv = nil
	p.httpSrvMu.Unlock()

	if srv == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func (p *Proxy) Snapshot() map[string]interface{} {
	return map[string]interface{}{
		"active_connections":     p.metrics.ActiveConnections.Load(),
		"proxy_requests_total":   p.metrics.TotalRequests.Load(),
		"proxy_blocked_total":    p.metrics.TotalBlocked.Load(),
		"proxy_upstream_errors":  p.metrics.UpstreamErrors.Load(),
		"mapping_degraded_count": p.metrics.MappingDegraded.Load(),
	}
}

func (p *Proxy) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (p *Proxy) handleProxy(w http.ResponseWriter, r *http.Request) {
	start := time.Now().UTC()
	p.metrics.ActiveConnections.Add(1)
	p.metrics.TotalRequests.Add(1)
	defer p.metrics.ActiveConnections.Add(-1)

	eventID := uuid.NewString()
	originalReqID := r.Header.Get("X-Request-ID")
	w.Header().Set("X-Request-ID", eventID)
	if originalReqID != "" {
		w.Header().Set("X-Crabwise-Original-Request-ID", originalReqID)
	}

	body, err := p.readRequestBody(w, r)
	if err != nil {
		return
	}

	p.providersMu.RLock()
	providerRuntime, providerName, err := p.router.Resolve(r)
	p.providersMu.RUnlock()
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, "routing_error", err.Error(), eventID)
		return
	}

	normalizedReq, mappingDegraded, reqMapErr := p.normalizeRequest(providerRuntime, providerName, r.URL.Path, body)
	if reqMapErr != nil && p.cfg.MappingStrictMode {
		writeProxyError(w, http.StatusBadGateway, "mapping_error", reqMapErr.Error(), eventID)
		return
	}
	if mappingDegraded {
		p.metrics.MappingDegraded.Add(1)
	}

	preflight := p.buildAuditEvent(eventID, start, providerName, normalizedReq, r.URL.Path)
	blocked := p.shouldBlock(preflight)
	if blocked {
		p.metrics.TotalBlocked.Add(1)
		preflight.Outcome = audit.OutcomeBlocked
		p.appendArgumentMetadata(preflight, map[string]interface{}{
			"request_id":       eventID,
			"endpoint":         r.URL.Path,
			"mapping_degraded": mappingDegraded,
		})
		p.maybeWriteRawPayload(preflight, body)
		p.emit(preflight)
		writeProxyError(w, http.StatusForbidden, "policy_violation", "request blocked by commandments", eventID)
		return
	}

	if p.cfg.RedactEgressDefault {
		redacted, changed := RedactPayload(body, p.extraRedactREs)
		if changed {
			body = redacted
			p.appendArgumentMetadata(preflight, map[string]interface{}{"egress_redacted": true})
		}
	}

	upstreamReq, err := p.buildUpstreamRequest(r, providerRuntime, body, eventID)
	if err != nil {
		writeProxyError(w, http.StatusBadGateway, "upstream_request_error", err.Error(), eventID)
		preflight.Outcome = audit.OutcomeFailure
		p.appendArgumentMetadata(preflight, map[string]interface{}{
			"request_id": eventID,
			"error_type": "upstream_request_error",
		})
		p.emit(preflight)
		return
	}
	if err := providerRuntime.Transport.PrepareAuth(upstreamReq); err != nil {
		writeProxyError(w, http.StatusBadGateway, "auth_error", err.Error(), eventID)
		preflight.Outcome = audit.OutcomeFailure
		p.appendArgumentMetadata(preflight, map[string]interface{}{
			"request_id":    eventID,
			"error_type":    "auth",
			"error_message": err.Error(),
		})
		p.emit(preflight)
		return
	}

	upstreamResp, err := providerRuntime.Transport.Forward(r.Context(), upstreamReq)
	if err != nil {
		p.metrics.UpstreamErrors.Add(1)
		writeProxyError(w, http.StatusBadGateway, "upstream_error", err.Error(), eventID)
		preflight.Outcome = audit.OutcomeFailure
		p.appendArgumentMetadata(preflight, map[string]interface{}{
			"request_id":    eventID,
			"error_type":    "upstream_error",
			"error_message": err.Error(),
		})
		p.emit(preflight)
		return
	}
	defer upstreamResp.Body.Close()

	normResp := NormalizedResponse{UpstreamStatus: upstreamResp.StatusCode, MappingDegraded: mappingDegraded}
	contentType := strings.ToLower(upstreamResp.Header.Get("Content-Type"))

	sendUpstreamHeaders := func(status int) {
		copyResponseHeaders(w.Header(), upstreamResp.Header)
		w.Header().Set("X-Request-ID", eventID)
		if originalReqID != "" {
			w.Header().Set("X-Crabwise-Original-Request-ID", originalReqID)
		}
		w.WriteHeader(status)
	}

	var firstTokenAt time.Time

	if normalizedReq.Stream || strings.Contains(contentType, "text/event-stream") {
		maxBuf := p.cfg.StreamMaxBuffer
		if maxBuf <= 0 {
			maxBuf = defaultStreamMaxBytes
		}
		buffered, streamErr := bufferSSEStream(upstreamResp.Body, providerRuntime.Transport, p.cfg.StreamIdleTimeout, maxBuf)
		if streamErr != nil {
			if strings.Contains(streamErr.Error(), "stream buffer exceeded") || strings.Contains(streamErr.Error(), "enforcement_error") {
				writeProxyError(w, http.StatusBadGateway, "enforcement_error", streamErr.Error(), eventID)
			} else {
				p.metrics.UpstreamErrors.Add(1)
				writeProxyError(w, http.StatusBadGateway, "upstream_error", streamErr.Error(), eventID)
			}
			preflight.Outcome = audit.OutcomeFailure
			p.emit(preflight)
			return
		}

		// Evaluate tool_use blocks from the buffered stream
		if blocked, commandmentID := p.evaluateToolUseBlocks(buffered.ToolBlocks, providerName, normalizedReq.Model, start); blocked {
			p.metrics.TotalBlocked.Add(1)
			writeProxyError(w, http.StatusForbidden, "policy_violation",
				"tool_use blocked by commandment: "+commandmentID, eventID)
			preflight.Outcome = audit.OutcomeBlocked
			p.appendArgumentMetadata(preflight, map[string]interface{}{
				"blocked_commandment": commandmentID,
				"enforcement":         "response_side_stream",
			})
			p.emit(preflight)
			return
		}

		// Forward buffered stream to client
		sendUpstreamHeaders(upstreamResp.StatusCode)
		if _, err := w.Write(buffered.Body); err != nil {
			p.metrics.UpstreamErrors.Add(1)
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		streamTel := buffered.Telemetry
		normResp.InputTokens = streamTel.InputTokens
		normResp.OutputTokens = streamTel.OutputTokens
		normResp.FinishReason = streamTel.FinishReason
		if streamTel.Model != "" {
			normResp.Model = streamTel.Model
		}
		firstTokenAt = streamTel.FirstTokenAt
	} else {
		respBody, readErr := io.ReadAll(upstreamResp.Body)
		if readErr != nil {
			p.metrics.UpstreamErrors.Add(1)
			normResp.ErrorType = "read_error"
			normResp.ErrorMessage = readErr.Error()
		}

		// Response-side tool_use enforcement (non-streaming)
		if len(respBody) > 0 && upstreamResp.StatusCode < 400 {
			toolUseBlocks, extractErr := providerRuntime.Transport.ExtractToolUseBlocks(respBody)
			if extractErr != nil {
				writeProxyError(w, http.StatusBadGateway, "enforcement_error",
					"failed to extract tool_use blocks: "+extractErr.Error(), eventID)
				preflight.Outcome = audit.OutcomeFailure
				p.emit(preflight)
				return
			}
			if blocked, commandmentID := p.evaluateToolUseBlocks(toolUseBlocks, providerName, normalizedReq.Model, start); blocked {
				p.metrics.TotalBlocked.Add(1)
				writeProxyError(w, http.StatusForbidden, "policy_violation",
					"tool_use blocked by commandment: "+commandmentID, eventID)
				preflight.Outcome = audit.OutcomeBlocked
				p.appendArgumentMetadata(preflight, map[string]interface{}{
					"blocked_commandment": commandmentID,
					"enforcement":         "response_side",
				})
				p.emit(preflight)
				return
			}
		}

		if len(respBody) > 0 {
			responseMapped, respMapErr := NormalizeResponse(providerRuntime.Mapping, respBody, upstreamResp.StatusCode)
			if respMapErr != nil {
				normResp.MappingDegraded = true
				normResp.ErrorType = "mapping_error"
				normResp.ErrorMessage = "response mapping failed: " + respMapErr.Error()
				p.metrics.MappingDegraded.Add(1)
				if p.cfg.MappingStrictMode {
					writeProxyError(w, http.StatusBadGateway, "mapping_error", normResp.ErrorMessage, eventID)
				} else {
					sendUpstreamHeaders(upstreamResp.StatusCode)
					_, _ = w.Write(respBody)
				}
			} else {
				sendUpstreamHeaders(upstreamResp.StatusCode)
				_, _ = w.Write(respBody)
				normResp = responseMapped
				normResp.MappingDegraded = mappingDegraded
			}
		} else {
			sendUpstreamHeaders(upstreamResp.StatusCode)
		}
	}

	if normResp.Model != "" {
		preflight.Model = normResp.Model
	}
	preflight.InputTokens = normResp.InputTokens
	preflight.OutputTokens = normResp.OutputTokens

	costResult := ComputeCost(p.cfg.Pricing, preflight.Model, preflight.InputTokens, preflight.OutputTokens)
	preflight.CostUSD = costResult.CostUSD

	meta := map[string]interface{}{
		"request_id":       eventID,
		"endpoint":         r.URL.Path,
		"upstream_status":  upstreamResp.StatusCode,
		"mapping_degraded": normResp.MappingDegraded,
	}
	if normResp.FinishReason != "" {
		meta["finish_reason"] = normResp.FinishReason
	}
	if normResp.ErrorType != "" {
		meta["error_type"] = normalizeErrorType(normResp.ErrorType, upstreamResp.StatusCode)
	}
	if normResp.ErrorMessage != "" {
		meta["error_message"] = normResp.ErrorMessage
	}
	if costResult.UnknownModel {
		meta["cost_unknown_model"] = true
	}
	if !firstTokenAt.IsZero() {
		meta["first_token_ms"] = firstTokenAt.Sub(start).Milliseconds()
	}

	if upstreamResp.StatusCode >= 400 || normResp.ErrorType != "" {
		preflight.Outcome = audit.OutcomeFailure
	} else if preflight.Outcome == "" {
		preflight.Outcome = audit.OutcomeSuccess
	}
	p.appendArgumentMetadata(preflight, meta)
	p.maybeWriteRawPayload(preflight, body)

	crabwiseOtel.EmitGenAISpan(r.Context(), crabwiseOtel.GenAISpanData{
		System:        providerName,
		Operation:     endpointAction(r.URL.Path),
		RequestModel:  preflight.Model,
		ResponseModel: normResp.Model,
		FinishReason:  normResp.FinishReason,
		InputTokens:   preflight.InputTokens,
		OutputTokens:  preflight.OutputTokens,
		CostUSD:       preflight.CostUSD,
		Outcome:       string(preflight.Outcome),
		Provider:      providerName,
		AdapterID:     "proxy",
	})

	p.emit(preflight)
}

func (p *Proxy) readRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, p.cfg.MaxRequestBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProxyError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds max_request_body", "")
		return nil, err
	}
	return body, nil
}

func (p *Proxy) normalizeRequest(runtime *ProviderRuntime, provider, endpoint string, body []byte) (NormalizedRequest, bool, error) {
	normalized, err := NormalizeRequest(runtime.Mapping, provider, endpoint, body)
	if err != nil {
		normalized = NormalizedRequest{
			Provider:        provider,
			Endpoint:        endpoint,
			MappingDegraded: true,
		}
		return normalized, true, err
	}

	for i := range normalized.Tools {
		tool := &normalized.Tools[i]
		argKeys := classify.ExtractArgKeys(tool.RawArgs)
		result := p.classifier.Classify(provider, tool.Name, argKeys)
		tool.ArgKeys = argKeys
		tool.Category = result.Category
		tool.Effect = result.Effect
		tool.TaxonomyVersion = result.TaxonomyVersion
		tool.ClassificationSource = result.ClassificationSource
	}

	return normalized, false, nil
}

func (p *Proxy) buildAuditEvent(eventID string, ts time.Time, provider string, req NormalizedRequest, endpoint string) *audit.AuditEvent {
	e := &audit.AuditEvent{
		ID:          eventID,
		Timestamp:   ts,
		AgentID:     "proxy",
		ActionType:  audit.ActionAIRequest,
		Action:      endpointAction(endpoint),
		Outcome:     audit.OutcomeSuccess,
		Provider:    provider,
		Model:       req.Model,
		AdapterID:   "proxy",
		AdapterType: "proxy",
	}

	if p.attributor != nil {
		if match, ok := p.attributor.MatchProxyRequest(ts, provider, req.Model); ok {
			confidence := "high"
			if match.SessionKey == "" {
				confidence = "low"
			}
			if match.AgentID != "" {
				e.AgentID = match.AgentID
			}
			e.SessionID = match.SessionKey
			e.ParentSessionID = match.ParentSession
			if e.Model == "" && match.Model != "" {
				e.Model = match.Model
			}
			p.appendArgumentMetadata(e, map[string]interface{}{
				"openclaw.run_id":                 match.RunID,
				"openclaw.session_key":            match.SessionKey,
				"openclaw.agent_id":               match.AgentID,
				"openclaw.thinking_level":         match.ThinkingLevel,
				"openclaw.correlation_confidence": confidence,
			})
		}
	}

	if len(req.Tools) > 0 {
		first := req.Tools[0]
		e.ToolName = first.Name
		e.ToolCategory = first.Category
		e.ToolEffect = first.Effect
		e.TaxonomyVersion = first.TaxonomyVersion
		e.ClassificationSource = first.ClassificationSource
	}

	if len(req.Tools) > 1 {
		toolsSummary := make([]map[string]string, len(req.Tools))
		for i, t := range req.Tools {
			toolsSummary[i] = map[string]string{
				"name":     t.Name,
				"category": t.Category,
				"effect":   t.Effect,
			}
		}
		p.appendArgumentMetadata(e, map[string]interface{}{"tools": toolsSummary})
	}

	return e
}

func (p *Proxy) shouldBlock(event *audit.AuditEvent) bool {
	if p.evaluator == nil || event == nil {
		return false
	}
	result := p.evaluator.Evaluate(event)
	for _, triggered := range result.Triggered {
		if strings.EqualFold(triggered.Enforcement, "block") {
			return true
		}
	}
	return false
}

// evaluateToolUseBlocks evaluates each tool_use block from an LLM response.
// Returns (true, commandmentID) if any block is blocked; emits audit events for blocked blocks.
// Returns (false, "") if all pass. Pass-through events are not emitted (too high volume).
func (p *Proxy) evaluateToolUseBlocks(blocks []ToolUseBlock, provider, model string, ts time.Time) (bool, string) {
	if p.evaluator == nil {
		return false, ""
	}
	for _, block := range blocks {
		argKeys := classify.ExtractArgKeys(block.ToolInput)
		var cls classify.ClassifyResult
		if p.classifier != nil {
			cls = p.classifier.Classify(provider, block.ToolName, argKeys)
		}

		e := &audit.AuditEvent{
			ID:                   uuid.NewString(),
			Timestamp:            ts,
			AgentID:              "proxy",
			ActionType:           audit.ActionToolCall,
			Action:               block.ToolName,
			Provider:             provider,
			Model:                model,
			ToolName:             block.ToolName,
			ToolCategory:         cls.Category,
			ToolEffect:           cls.Effect,
			TaxonomyVersion:      cls.TaxonomyVersion,
			ClassificationSource: cls.ClassificationSource,
			AdapterID:            "proxy",
			AdapterType:          "proxy",
		}
		p.appendArgumentMetadata(e, map[string]interface{}{
			"tool_call_id": block.ID,
			"tool_input":   block.ToolInput,
			"targets":      block.Targets,
		})

		result := p.evaluator.Evaluate(e)
		for _, triggered := range result.Triggered {
			if strings.EqualFold(triggered.Enforcement, "block") {
				e.Outcome = audit.OutcomeBlocked
				p.emit(e)
				return true, triggered.Name
			}
		}
	}
	return false, ""
}

func (p *Proxy) emit(e *audit.AuditEvent) {
	if p.events == nil || e == nil {
		return
	}
	p.events <- e
}

func (p *Proxy) maybeWriteRawPayload(e *audit.AuditEvent, body []byte) {
	if p.rawPayloads == nil || e == nil || len(body) == 0 {
		return
	}
	ref, err := p.rawPayloads.Write(e.ID, body)
	if err != nil {
		log.Printf("proxy: raw payload write error: %v", err)
		return
	}
	if ref != "" {
		e.RawPayloadRef = ref
	}
}

func (p *Proxy) buildUpstreamRequest(incoming *http.Request, runtime *ProviderRuntime, body []byte, requestID string) (*http.Request, error) {
	target, err := joinTargetURL(runtime.Config.UpstreamBaseURL, incoming.URL.Path, incoming.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	outReq, err := http.NewRequestWithContext(incoming.Context(), incoming.Method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyForwardHeaders(outReq.Header, incoming.Header)
	outReq.Header.Set("X-Request-ID", requestID)
	if original := incoming.Header.Get("X-Request-ID"); original != "" {
		outReq.Header.Set("X-Crabwise-Original-Request-ID", original)
	}
	return outReq, nil
}

func joinTargetURL(baseURL, requestPath, rawQuery string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("invalid upstream base url: %w", err)
	}
	u.Path = path.Join(u.Path, requestPath)
	u.RawQuery = rawQuery
	return u.String(), nil
}

func writeProxyError(w http.ResponseWriter, status int, errType, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	if requestID != "" {
		w.Header().Set("X-Request-ID", requestID)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"type":    errType,
			"message": message,
		},
		"request_id": requestID,
	})
}

func endpointAction(endpoint string) string {
	switch endpoint {
	case "/v1/chat/completions":
		return "chat.completions.create"
	case "/v1/responses":
		return "responses.create"
	default:
		return strings.TrimPrefix(endpoint, "/")
	}
}

func normalizeErrorType(raw string, status int) string {
	switch strings.ToLower(raw) {
	case "rate_limit", "rate_limit_exceeded":
		return "rate_limit"
	case "server_error":
		return "server_error"
	case "invalid_request", "invalid_request_error":
		return "invalid_request"
	case "auth", "authentication_error":
		return "auth"
	}

	switch status {
	case http.StatusTooManyRequests:
		return "rate_limit"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "auth"
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "server_error"
	default:
		return "unknown"
	}
}

func copyForwardHeaders(dst, src http.Header) {
	for key, values := range src {
		lk := strings.ToLower(key)
		if lk == "host" || lk == "content-length" || lk == "connection" || lk == "transfer-encoding" {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		lk := strings.ToLower(key)
		if lk == "content-length" || lk == "connection" || lk == "transfer-encoding" {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

func (p *Proxy) appendArgumentMetadata(e *audit.AuditEvent, kv map[string]interface{}) {
	if e == nil || len(kv) == 0 {
		return
	}

	existing := map[string]interface{}{}
	if strings.TrimSpace(e.Arguments) != "" {
		_ = json.Unmarshal([]byte(e.Arguments), &existing)
	}
	for k, v := range kv {
		existing[k] = v
	}
	b, err := json.Marshal(existing)
	if err != nil {
		return
	}
	e.Arguments = string(b)
}

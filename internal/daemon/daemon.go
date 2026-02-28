package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crabwise-ai/crabwise/configs"
	"github.com/crabwise-ai/crabwise/internal/adapter/logwatcher"
	"github.com/crabwise-ai/crabwise/internal/adapter/openclaw"
	"github.com/crabwise-ai/crabwise/internal/adapter/proxy"
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/classify"
	"github.com/crabwise-ai/crabwise/internal/discovery"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/openclawstate"
	crabwiseOtel "github.com/crabwise-ai/crabwise/internal/otel"
	"github.com/crabwise-ai/crabwise/internal/queue"
	"github.com/crabwise-ai/crabwise/internal/store"
)

// Version is set by the CLI layer at startup to avoid import cycles.
var Version = "dev"

type Daemon struct {
	cfg           *Config
	store         *store.Store
	queue         *queue.Queue
	logger        *audit.Logger
	ipcServer     *ipc.Server
	registry      *discovery.Registry
	watcher       *logwatcher.LogWatcher
	proxy         *proxy.Proxy
	openclaw      *openclaw.Adapter
	openclawState *openclawstate.Store
	commandments  CommandmentsService
	classifier    classify.Classifier
	startTime     time.Time

	hostname string
	userID   string

	eventCh chan *audit.AuditEvent
	cancel  context.CancelFunc
}

func New(cfg *Config) *Daemon {
	return &Daemon{
		cfg:      cfg,
		registry: discovery.NewRegistry(),
		eventCh:  make(chan *audit.AuditEvent, 1000),
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	ctx, d.cancel = context.WithCancel(ctx)
	d.startTime = time.Now()

	// Resolve origin identity once at startup — UID is kernel-verified, not spoofable
	d.hostname, _ = os.Hostname()
	d.userID = strconv.Itoa(os.Getuid())

	// PID file
	if err := d.writePID(); err != nil {
		return fmt.Errorf("pid file: %w", err)
	}
	defer d.removePID()

	// SQLite
	s, err := store.Open(d.cfg.Daemon.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	d.store = s
	defer s.Close()

	// OTel TracerProvider
	otelShutdown, otelErr := crabwiseOtel.Init(ctx, crabwiseOtel.Config{
		Enabled:        d.cfg.OTel.Enabled,
		Endpoint:       d.cfg.OTel.Endpoint,
		ExportInterval: d.cfg.OTel.ExportInterval.Duration(),
		ServiceName:    "crabwise",
		ServiceVersion: Version,
	})
	if otelErr != nil {
		return fmt.Errorf("otel init: %w", otelErr)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			log.Printf("daemon: otel shutdown: %v", err)
		}
	}()

	// Queue
	policy := queue.PolicyBlockWithTimeout
	if d.cfg.Queue.Overflow == "drop_oldest" {
		policy = queue.PolicyDropOldest
	}
	d.queue = queue.New(d.cfg.Queue.Capacity, policy, d.cfg.Queue.BlockTimeout.Duration())

	// Audit logger
	d.logger, err = audit.NewLogger(s, d.queue, d.cfg.Queue.BatchSize, d.cfg.Queue.FlushInterval.Duration())
	if err != nil {
		return fmt.Errorf("audit logger: %w", err)
	}

	var commandmentsInitErr error
	if d.commandments, commandmentsInitErr = NewCommandmentsService(d.cfg.Commandments.File, DefaultCommandmentsYAML); commandmentsInitErr != nil {
		log.Printf("daemon: commandments init error: %v", commandmentsInitErr)
		d.commandments = &noopCommandmentsService{}
	}
	d.logger.SetEvaluator(d.commandments)
	d.logger.SetRedactor(d.commandments)

	registry, registryInitErr := d.loadToolRegistryCandidate()
	if registryInitErr != nil {
		log.Printf("daemon: tool registry init error: %v", registryInitErr)
		fallbackRegistry, fallbackErr := classify.LoadRegistry("", d.toolRegistryFallbackYAML())
		if fallbackErr != nil {
			log.Printf("daemon: embedded tool registry fallback error: %v", fallbackErr)
			d.classifier = classify.NewFallbackRegistry()
		} else {
			d.classifier = fallbackRegistry
		}
	} else {
		d.classifier = registry
	}
	logwatcher.SetClassifier(d.classifier)

	d.logger.Start(ctx)
	defer d.logger.Stop()

	if commandmentsInitErr != nil {
		d.emitSystemEvent("commandments_load_failed", audit.OutcomeFailure, map[string]interface{}{"error": commandmentsInitErr.Error()})
	} else {
		rulesLoaded := len(d.commandments.List())
		d.emitSystemEvent("commandments_load_ok", audit.OutcomeSuccess, map[string]interface{}{"rules_loaded": rulesLoaded})
	}

	// IPC server
	d.ipcServer = ipc.NewServer(d.cfg.Daemon.SocketPath)
	d.registerIPC()
	if err := d.ipcServer.Start(); err != nil {
		return fmt.Errorf("ipc server: %w", err)
	}
	defer func() {
		if err := d.ipcServer.Stop(); err != nil {
			log.Printf("daemon: ipc server stop: %v", err)
		}
	}()

	// Log watcher adapter
	d.watcher = logwatcher.New(
		d.cfg.Discovery.LogPaths,
		d.cfg.Adapters.LogWatcher.PollFallbackInterval.Duration(),
		d.store,
	)
	if d.cfg.Adapters.LogWatcher.Enabled {
		if err := d.watcher.Start(ctx, d.eventCh); err != nil {
			log.Printf("daemon: log watcher start error: %v", err)
		}
		defer func() {
			if err := d.watcher.Stop(); err != nil {
				log.Printf("daemon: watcher stop: %v", err)
			}
		}()
	}

	if d.cfg.Adapters.Proxy.Enabled {
		px, err := proxy.New(d.proxyConfig(), d.commandments, d.classifier, d.eventCh)
		if err != nil {
			log.Printf("daemon: proxy init error: %v", err)
		} else {
			d.proxy = px
			if d.cfg.Audit.RawPayloadEnabled {
				rpm := audit.NewRawPayloadManager(
					d.cfg.Daemon.RawPayloadDir,
					d.cfg.Audit.RawPayloadMaxSize,
					d.cfg.Audit.RawPayloadQuota,
					d.cfg.Audit.RetentionDays,
				)
				d.proxy.SetRawPayloadWriter(rpm)
			}
			go func() {
				if err := d.proxy.Start(ctx); err != nil {
					log.Printf("daemon: proxy server error: %v", err)
				}
			}()
			defer func() {
				if err := d.proxy.Stop(); err != nil {
					log.Printf("daemon: proxy stop: %v", err)
				}
			}()
		}
	}

	// Event forwarder: eventCh → queue
	go d.forwardEvents(ctx)

	if err := d.startOpenClaw(ctx); err != nil {
		log.Printf("daemon: openclaw adapter start error: %v", err)
	} else if d.openclaw != nil {
		defer func() {
			if err := d.openclaw.Stop(); err != nil {
				log.Printf("daemon: openclaw stop: %v", err)
			}
		}()
	}

	// Discovery scanner
	go d.discoveryLoop(ctx)

	log.Printf("daemon: started (pid=%d, socket=%s, db=%s)", os.Getpid(), d.cfg.Daemon.SocketPath, d.cfg.Daemon.DBPath)

	// Wait for signal loop
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				if _, err := d.reloadRuntime(); err != nil {
					log.Printf("daemon: runtime reload error: %v", err)
				}
			default:
				log.Printf("daemon: received %s, shutting down", sig)
				d.cancel()
				return nil
			}
		case <-ctx.Done():
			log.Printf("daemon: context cancelled, shutting down")
			d.cancel()
			return nil
		}
	}
}

func (d *Daemon) forwardEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-d.eventCh:
			if !ok {
				return
			}
			evt.Hostname = d.hostname
			evt.UserID = d.userID
			d.queue.Send(evt)
		}
	}
}

func (d *Daemon) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.Discovery.ScanInterval.Duration())
	defer ticker.Stop()

	scan := func() {
		var agents []discovery.AgentInfo
		agents = append(agents, discovery.ScanProcesses(d.cfg.Discovery.ProcessSignatures)...)
		agents = append(agents, discovery.ScanLogPaths(d.cfg.Discovery.LogPaths)...)
		d.registry.ReplaceSource("scanner", agents)
	}

	scan() // initial scan
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scan()
		}
	}
}

func (d *Daemon) registerIPC() {
	d.ipcServer.Handle("status", func(params json.RawMessage) (interface{}, error) {
		return d.statusSnapshot(), nil
	})

	d.ipcServer.Handle("agents.list", func(params json.RawMessage) (interface{}, error) {
		return d.registry.List(), nil
	})

	d.ipcServer.Handle("audit.query", func(params json.RawMessage) (interface{}, error) {
		var filter audit.QueryFilter
		if len(params) > 0 {
			var f struct {
				Since     string `json:"since"`
				Until     string `json:"until"`
				Agent     string `json:"agent"`
				Action    string `json:"action"`
				Session   string `json:"session"`
				Outcome   string `json:"outcome"`
				Triggered bool   `json:"triggered"`
				Limit     int    `json:"limit"`
				Offset    int    `json:"offset"`
			}
			if err := json.Unmarshal(params, &f); err != nil {
				return nil, fmt.Errorf("parse params: %w", err)
			}
			if f.Since != "" {
				t, _ := time.Parse(time.RFC3339, f.Since)
				filter.Since = &t
			}
			if f.Until != "" {
				t, _ := time.Parse(time.RFC3339, f.Until)
				filter.Until = &t
			}
			filter.Agent = f.Agent
			filter.Action = f.Action
			filter.Session = f.Session
			filter.Outcome = f.Outcome
			filter.TriggeredOnly = f.Triggered
			filter.Limit = f.Limit
			filter.Offset = f.Offset
		}
		return audit.QueryEvents(d.store.DB(), filter)
	})

	d.ipcServer.Handle("commandments.list", func(params json.RawMessage) (interface{}, error) {
		if d.commandments == nil {
			return []CommandmentRuleSummary{}, nil
		}
		return d.commandments.List(), nil
	})

	d.ipcServer.Handle("commandments.test", func(params json.RawMessage) (interface{}, error) {
		if d.commandments == nil {
			return audit.EvalResult{Evaluated: []string{}, Triggered: []audit.TriggeredRule{}}, nil
		}

		if len(params) == 0 {
			return nil, fmt.Errorf("missing params")
		}

		var event audit.AuditEvent
		var payload struct {
			Event json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(params, &payload); err == nil && len(payload.Event) > 0 {
			if err := json.Unmarshal(payload.Event, &event); err != nil {
				return nil, fmt.Errorf("parse event: %w", err)
			}
		} else {
			if err := json.Unmarshal(params, &event); err != nil {
				return nil, fmt.Errorf("parse event: %w", err)
			}
		}

		return d.commandments.Test(&event), nil
	})

	d.ipcServer.Handle("commandments.reload", func(params json.RawMessage) (interface{}, error) {
		rulesLoaded, err := d.reloadRuntime()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "rules_loaded": rulesLoaded}, nil
	})

	d.ipcServer.Handle("audit.verify", func(params json.RawMessage) (interface{}, error) {
		valid, total, brokenAt, err := audit.VerifyIntegrity(d.store.DB())
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"valid":     valid,
			"total":     total,
			"broken_at": brokenAt,
		}, nil
	})

	d.ipcServer.Handle("audit.export", func(params json.RawMessage) (interface{}, error) {
		result, err := audit.QueryEvents(d.store.DB(), audit.QueryFilter{})
		if err != nil {
			return nil, err
		}
		return result.Events, nil
	})

	d.ipcServer.Handle("audit.cost", func(params json.RawMessage) (interface{}, error) {
		var filter audit.QueryFilter
		if len(params) > 0 {
			var f struct {
				Since string `json:"since"`
				Until string `json:"until"`
				Agent string `json:"agent"`
			}
			if err := json.Unmarshal(params, &f); err != nil {
				return nil, fmt.Errorf("parse params: %w", err)
			}
			if f.Since != "" {
				t, _ := time.Parse(time.RFC3339, f.Since)
				filter.Since = &t
			}
			if f.Until != "" {
				t, _ := time.Parse(time.RFC3339, f.Until)
				filter.Until = &t
			}
			filter.Agent = f.Agent
		}
		return audit.QueryCostSummary(d.store.DB(), filter)
	})

	d.ipcServer.HandleSubscribe(func(params json.RawMessage, send func(string, interface{}) error, done <-chan struct{}) error {
		ch := d.logger.Subscribe()
		defer d.logger.Unsubscribe(ch)

		for {
			select {
			case <-done:
				return nil
			case evt, ok := <-ch:
				if !ok {
					return nil
				}
				if err := send("audit.event", evt); err != nil {
					return err
				}
			}
		}
	})
}

func (d *Daemon) writePID() error {
	dir := filepath.Dir(d.cfg.Daemon.PIDFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Check if already running
	if data, err := os.ReadFile(d.cfg.Daemon.PIDFile); err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pid, err := strconv.Atoi(pidStr); err == nil {
			if process, err := os.FindProcess(pid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					return fmt.Errorf("daemon already running (pid %d)", pid)
				}
			}
		}
	}

	return os.WriteFile(d.cfg.Daemon.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0600)
}

func (d *Daemon) removePID() {
	os.Remove(d.cfg.Daemon.PIDFile)
}

func (d *Daemon) reloadRuntime() (int, error) {
	newCommandments, cmdErr := NewCommandmentsService(d.cfg.Commandments.File, DefaultCommandmentsYAML)
	if cmdErr != nil {
		d.emitSystemEvent("commandments_reload_failed", audit.OutcomeFailure, map[string]interface{}{"error": cmdErr.Error()})
	}

	newRegistry, regErr := d.loadToolRegistryCandidate()
	if regErr != nil {
		d.emitSystemEvent("tool_registry_reload_failed", audit.OutcomeFailure, map[string]interface{}{"error": regErr.Error()})
	}

	if cmdErr != nil || regErr != nil {
		if cmdErr != nil && regErr != nil {
			return 0, fmt.Errorf("runtime reload failed: %w", errors.Join(
				fmt.Errorf("reload commandments: %w", cmdErr),
				fmt.Errorf("reload tool registry: %w", regErr),
			))
		}
		if cmdErr != nil {
			return 0, fmt.Errorf("runtime reload failed: reload commandments: %w", cmdErr)
		}
		return 0, fmt.Errorf("runtime reload failed: reload tool registry: %w", regErr)
	}

	d.commandments = newCommandments
	d.logger.SetEvaluator(d.commandments)
	d.logger.SetRedactor(d.commandments)
	if d.proxy != nil {
		d.proxy.SetEvaluator(d.commandments)
	}

	d.classifier = newRegistry
	logwatcher.SetClassifier(d.classifier)
	if d.proxy != nil {
		d.proxy.SetClassifier(d.classifier)
		if mapErr := d.proxy.ReloadMappings(); mapErr != nil {
			log.Printf("daemon: proxy mapping reload error: %v", mapErr)
			d.emitSystemEvent("proxy_mappings_reload_failed", audit.OutcomeFailure, map[string]interface{}{"error": mapErr.Error()})
		} else {
			d.emitSystemEvent("proxy_mappings_reload_ok", audit.OutcomeSuccess, nil)
		}
	}

	rulesLoaded := len(d.commandments.List())
	d.emitSystemEvent("commandments_reload_ok", audit.OutcomeSuccess, map[string]interface{}{"rules_loaded": rulesLoaded})
	d.emitSystemEvent("tool_registry_reload_ok", audit.OutcomeSuccess, map[string]interface{}{"version": newRegistry.Version()})

	return rulesLoaded, nil
}

func (d *Daemon) startOpenClaw(ctx context.Context) error {
	if d.cfg == nil || !d.cfg.Adapters.OpenClaw.Enabled {
		return nil
	}
	if d.registry == nil {
		d.registry = discovery.NewRegistry()
	}
	if d.eventCh == nil {
		d.eventCh = make(chan *audit.AuditEvent, 1000)
	}

	cfg := openclaw.ResolveConfigEnv(openclaw.Config{
		Enabled:                d.cfg.Adapters.OpenClaw.Enabled,
		GatewayURL:             d.cfg.Adapters.OpenClaw.GatewayURL,
		APITokenEnv:            d.cfg.Adapters.OpenClaw.APITokenEnv,
		SessionRefreshInterval: d.cfg.Adapters.OpenClaw.SessionRefreshInterval.Duration(),
		CorrelationWindow:      d.cfg.Adapters.OpenClaw.CorrelationWindow.Duration(),
	})

	d.openclawState = openclawstate.New(cfg.CorrelationWindow)
	if d.proxy != nil {
		d.proxy.SetRequestAttributor(d.openclawState)
	}
	adapter := openclaw.NewAdapter(cfg, d.openclawState)
	adapter.SetSessionObserver(func(sessions []openclaw.SessionInfo) {
		d.registry.ReplaceSource("openclaw-gateway", openclawSessionsToAgents(sessions))
	})
	if err := adapter.Start(ctx, d.eventCh); err != nil {
		return err
	}
	d.openclaw = adapter
	return nil
}

func (d *Daemon) statusSnapshot() map[string]interface{} {
	var (
		queueDepth   uint64
		queueDropped uint64
	)
	if d.queue != nil {
		stats := d.queue.Stats()
		queueDepth = uint64(stats.Depth)
		queueDropped = stats.Dropped
	}

	var unclassified uint64
	if d.classifier != nil {
		unclassified = d.classifier.UnclassifiedCount()
	}

	resp := map[string]interface{}{
		"uptime":                         time.Since(d.startTime).Truncate(time.Second).String(),
		"agents":                         0,
		"queue_depth":                    queueDepth,
		"queue_dropped":                  queueDropped,
		"pid":                            os.Getpid(),
		"unclassified_tool_count":        unclassified,
		"openclaw_connected":             float64(0),
		"openclaw_session_cache_size":    float64(0),
		"openclaw_correlation_matches":   float64(0),
		"openclaw_correlation_ambiguous": float64(0),
	}
	if d.registry != nil {
		resp["agents"] = d.registry.Count()
	}
	if d.proxy != nil {
		for k, v := range d.proxy.Snapshot() {
			resp[k] = v
		}
	}
	if d.openclaw != nil && d.openclaw.Connected() {
		resp["openclaw_connected"] = float64(1)
		resp["openclaw_session_cache_size"] = float64(d.openclaw.SessionCacheSize())
	}
	if d.openclawState != nil {
		stats := d.openclawState.Stats()
		if resp["openclaw_session_cache_size"] == float64(0) {
			resp["openclaw_session_cache_size"] = float64(stats.SessionCount)
		}
		resp["openclaw_correlation_matches"] = float64(stats.Matches)
		resp["openclaw_correlation_ambiguous"] = float64(stats.Ambiguous)
	}
	return resp
}

func (d *Daemon) proxyConfig() proxy.Config {
	providers := make(map[string]proxy.ProviderConfig, len(d.cfg.Adapters.Proxy.Providers))
	for name, p := range d.cfg.Adapters.Proxy.Providers {
		authMode := p.AuthMode
		if authMode == "" {
			authMode = "passthrough"
		}
		providers[name] = proxy.ProviderConfig{
			Name:            name,
			UpstreamBaseURL: p.UpstreamBaseURL,
			AuthMode:        authMode,
			AuthKey:         p.AuthKey,
			RoutePatterns:   append([]string(nil), p.RoutePatterns...),
			MaxIdleConns:    p.MaxIdleConns,
			IdleConnTimeout: p.IdleConnTimeout.Duration(),
		}
	}

	pricing := make(map[string]proxy.Pricing, len(d.cfg.Cost.Pricing))
	for model, p := range d.cfg.Cost.Pricing {
		pricing[model] = proxy.Pricing{
			InputPerMillion:  p.Input,
			OutputPerMillion: p.Output,
		}
	}

	return proxy.Config{
		Listen:              d.cfg.Adapters.Proxy.Listen,
		DefaultProvider:     d.cfg.Adapters.Proxy.DefaultProvider,
		UpstreamTimeout:     d.cfg.Adapters.Proxy.UpstreamTimeout.Duration(),
		StreamIdleTimeout:   d.cfg.Adapters.Proxy.StreamIdleTimeout.Duration(),
		MaxRequestBody:      d.cfg.Adapters.Proxy.MaxRequestBody,
		RedactEgressDefault: d.cfg.Adapters.Proxy.RedactEgressDefault,
		RedactPatterns:      d.cfg.Adapters.Proxy.RedactPatterns,
		CACert:              d.cfg.Adapters.Proxy.CACert,
		CAKey:               d.cfg.Adapters.Proxy.CAKey,
		MappingsDir:         d.cfg.Adapters.Proxy.MappingsDir,
		MappingStrictMode:   d.cfg.Adapters.Proxy.MappingStrictMode,
		Providers:           providers,
		Pricing:             pricing,
	}
}

func openclawSessionsToAgents(sessions []openclaw.SessionInfo) []discovery.AgentInfo {
	agents := make([]discovery.AgentInfo, 0, len(sessions))
	for _, session := range sessions {
		agents = append(agents, discovery.AgentInfo{
			ID:             "openclaw/" + session.Key,
			Type:           "openclaw",
			Status:         "inactive",
			LastActivityAt: time.UnixMilli(session.LastActivityAt),
			DiscoveredAt:   time.Now().UTC(),
		})
	}
	return agents
}

func (d *Daemon) loadToolRegistryCandidate() (*classify.Registry, error) {
	registry, err := classify.LoadRegistry(d.cfg.ToolRegistry.File, d.toolRegistryFallbackYAML())
	if err != nil {
		return nil, err
	}
	return registry, nil
}

func (d *Daemon) toolRegistryFallbackYAML() []byte {
	if len(DefaultToolRegistryYAML) > 0 {
		return DefaultToolRegistryYAML
	}
	return configs.DefaultToolRegistryYAML
}

func (d *Daemon) emitSystemEvent(action string, outcome audit.Outcome, payload interface{}) {
	if d.queue == nil {
		return
	}

	args := ""
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			args = string(b)
		}
	}

	now := time.Now().UTC()
	e := &audit.AuditEvent{
		ID:          fmt.Sprintf("sys_%d_%s", now.UnixNano(), action),
		Timestamp:   now,
		AgentID:     "crabwise",
		ActionType:  audit.ActionSystem,
		Action:      action,
		Arguments:   args,
		Outcome:     outcome,
		AdapterID:   "daemon",
		AdapterType: "daemon",
		Hostname:    d.hostname,
		UserID:      d.userID,
	}

	d.queue.Send(e)
}

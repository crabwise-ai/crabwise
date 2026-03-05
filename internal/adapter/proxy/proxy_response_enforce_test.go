package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

// blockBashEvaluator blocks any tool named "Bash".
type blockBashEvaluator struct{}

func (b *blockBashEvaluator) Evaluate(e *audit.AuditEvent) audit.EvalResult {
	if e.ToolName == "Bash" {
		return audit.EvalResult{
			Triggered: []audit.TriggeredRule{{Name: "no-bash", Enforcement: "block"}},
		}
	}
	return audit.EvalResult{}
}

func newTestProxy(t *testing.T, upstream *httptest.Server, eval Evaluator) *Proxy {
	t.Helper()
	cfg := Config{
		Listen:          "127.0.0.1:0",
		DefaultProvider: "openai",
		MaxRequestBody:  1 << 20,
		Providers: map[string]ProviderConfig{
			"openai": {
				Name:            "openai",
				UpstreamBaseURL: upstream.URL,
				AuthMode:        "passthrough",
				RoutePatterns:   []string{"api.openai.com"},
			},
		},
	}
	p, err := New(cfg, eval, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestHandleProxy_NonStreaming_BlockedToolUse(t *testing.T) {
	toolCallResp := `{
		"id": "chatcmpl-1",
		"choices": [{
			"message": {
				"role": "assistant",
				"tool_calls": [{"id":"call_1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"rm -rf /\"}"}}]
			},
			"finish_reason": "tool_calls"
		}],
		"model": "gpt-4o"
	}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(toolCallResp))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream, &blockBashEvaluator{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "api.openai.com"
	rr := httptest.NewRecorder()

	p.handleProxy(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
	var errResp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("expected JSON error response: %v", err)
	}
	errObj := errResp["error"].(map[string]interface{})
	if errObj["type"] != "policy_violation" {
		t.Errorf("expected error type policy_violation, got %v", errObj["type"])
	}
}

func TestHandleProxy_NonStreaming_AllowedToolUse(t *testing.T) {
	toolCallResp := `{
		"id": "chatcmpl-2",
		"choices": [{
			"message": {
				"role": "assistant",
				"tool_calls": [{"id":"call_2","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"/src/main.go\"}"}}]
			},
			"finish_reason": "tool_calls"
		}],
		"model": "gpt-4o"
	}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(toolCallResp))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream, &blockBashEvaluator{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "api.openai.com"
	rr := httptest.NewRecorder()

	p.handleProxy(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

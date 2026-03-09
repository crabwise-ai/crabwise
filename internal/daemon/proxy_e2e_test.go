package daemon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	proxyadapter "github.com/crabwise-ai/crabwise/internal/adapter/proxy"
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/ipc"
)

const testOpenAIMappingYAML = `
version: "1"
provider: openai
request:
  model: { path: "$.model" }
  stream: { path: "$.stream", default: false }
  tools:
    path: "$.tools"
    each:
      name: { path: "$.function.name" }
      raw_args: { path: "$.function.parameters", serialize: json }
  input_summary: { path: "$.messages[-1].content", truncate: 200 }
response:
  model: { path: "$.model" }
  finish_reason: { path: "$.choices[0].finish_reason" }
  usage:
    input_tokens: { path: "$.usage.prompt_tokens" }
    output_tokens: { path: "$.usage.completion_tokens" }
  error:
    error_type: { path: "$.error.type" }
    error_message: { path: "$.error.message" }
stream:
  usage:
    input_tokens: { path: "$.usage.prompt_tokens" }
    output_tokens: { path: "$.usage.completion_tokens" }
  finish_reason: { path: "$.choices[0].finish_reason" }
`

func TestDaemonProxyE2E_AllowPath(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-e2e-allow",
			"model": "gpt-4o",
			"choices": []map[string]any{
				{"finish_reason": "stop", "message": map[string]string{"content": "ok"}},
			},
			"usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 3},
		})
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	allowRules := "version: \"1\"\ncommandments: []\n"
	runtime := startDaemonProxyE2ERuntime(t, upstreamURL, allowRules)

	status, body := sendMITMChatCompletion(t, runtime.proxyAddr, upstreamURL, runtime.caPool)
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body=%s)", status, body)
	}

	if hits := upstreamHits.Load(); hits < 1 {
		t.Fatalf("expected upstream hit >= 1, got %d", hits)
	}

	event := waitForAuditAIRequestEvent(t, runtime.socketPath, audit.OutcomeSuccess, 10*time.Second)
	if event.ActionType != audit.ActionAIRequest {
		t.Fatalf("expected action_type ai_request, got %s", event.ActionType)
	}
	if event.Outcome != audit.OutcomeSuccess {
		t.Fatalf("expected outcome success, got %s", event.Outcome)
	}
}

func TestDaemonProxyE2E_BlockPath(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	blockRules := `version: "1"
commandments:
  - name: block-openai-ai-requests
    description: Block proxy ai_request events in e2e
    enforcement: block
    priority: 100
    enabled: true
    match:
      action_type: ai_request
      provider: openai
    redact: false
    message: "blocked by e2e"
`
	runtime := startDaemonProxyE2ERuntime(t, upstreamURL, blockRules)

	status, body := sendMITMChatCompletion(t, runtime.proxyAddr, upstreamURL, runtime.caPool)
	if status != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d (body=%s)", status, body)
	}

	if hits := upstreamHits.Load(); hits != 0 {
		t.Fatalf("expected upstream hit == 0, got %d", hits)
	}

	event := waitForAuditAIRequestEvent(t, runtime.socketPath, audit.OutcomeBlocked, 10*time.Second)
	if event.Outcome != audit.OutcomeBlocked {
		t.Fatalf("expected outcome blocked, got %s", event.Outcome)
	}

	var triggered []audit.TriggeredRule
	if err := json.Unmarshal([]byte(event.CommandmentsTriggered), &triggered); err != nil {
		t.Fatalf("parse commandments_triggered: %v (raw=%q)", err, event.CommandmentsTriggered)
	}
	if len(triggered) == 0 {
		t.Fatalf("expected at least one triggered rule, got %q", event.CommandmentsTriggered)
	}

	matched := false
	for _, rule := range triggered {
		if rule.Name == "block-openai-ai-requests" && strings.EqualFold(rule.Enforcement, "block") {
			matched = true
			if rule.Message != "blocked by e2e" {
				t.Fatalf("expected triggered rule message %q, got %q", "blocked by e2e", rule.Message)
			}
			break
		}
	}
	if !matched {
		t.Fatalf("expected blocked rule metadata in triggered set, got %+v", triggered)
	}
}

type daemonProxyE2ERuntime struct {
	socketPath string
	proxyAddr  string
	caPool     *x509.CertPool
}

func startDaemonProxyE2ERuntime(t *testing.T, upstreamURL, commandmentsYAML string) daemonProxyE2ERuntime {
	t.Helper()

	runtimeDir := shortRuntimeDir(t, "proxy-e2e")
	socketPath := filepath.Join(runtimeDir, "cw.sock")
	dbPath := filepath.Join(runtimeDir, "cw.db")
	pidPath := filepath.Join(runtimeDir, "cw.pid")
	rawPayloadDir := filepath.Join(runtimeDir, "raw")
	commandmentsPath := filepath.Join(runtimeDir, "commandments.yaml")
	mappingsDir := filepath.Join(runtimeDir, "mappings")
	caCertPath := filepath.Join(runtimeDir, "ca.crt")
	caKeyPath := filepath.Join(runtimeDir, "ca.key")

	if err := os.MkdirAll(mappingsDir, 0o700); err != nil {
		t.Fatalf("mkdir mappings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mappingsDir, "openai.yaml"), []byte(testOpenAIMappingYAML), 0o600); err != nil {
		t.Fatalf("write proxy mapping: %v", err)
	}
	if err := os.WriteFile(commandmentsPath, []byte(commandmentsYAML), 0o600); err != nil {
		t.Fatalf("write commandments file: %v", err)
	}
	if err := proxyadapter.GenerateCA(caCertPath, caKeyPath); err != nil {
		t.Fatalf("generate test CA: %v", err)
	}

	caPool := x509.NewCertPool()
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatalf("read generated CA cert: %v", err)
	}
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		t.Fatal("append generated CA cert to pool")
	}

	proxyAddr := freeLocalAddr(t)
	cfg := &Config{
		Daemon: DaemonConfig{
			SocketPath:    socketPath,
			DBPath:        dbPath,
			RawPayloadDir: rawPayloadDir,
			PIDFile:       pidPath,
			LogLevel:      "info",
		},
		Discovery: DiscoveryConfig{
			ScanInterval:      Duration(1 * time.Hour),
			ProcessSignatures: []string{},
			LogPaths:          []string{},
		},
		Adapters: AdaptersConfig{
			LogWatcher: LogWatcherConfig{
				Enabled:              false,
				PollFallbackInterval: Duration(50 * time.Millisecond),
			},
			Proxy: ProxyConfig{
				Enabled:           true,
				Listen:            proxyAddr,
				DefaultProvider:   "openai",
				UpstreamTimeout:   Duration(3 * time.Second),
				StreamIdleTimeout: Duration(3 * time.Second),
				MaxRequestBody:    1 << 20,
				CACert:            caCertPath,
				CAKey:             caKeyPath,
				MappingsDir:       mappingsDir,
				Providers: map[string]ProxyProviderConfig{
					"openai": {
						UpstreamBaseURL: upstreamURL,
						AuthMode:        "passthrough",
						RoutePatterns:   []string{"/v1/*"},
					},
				},
			},
		},
		Queue: QueueConfig{
			Capacity:      1024,
			BatchSize:     1,
			FlushInterval: Duration(20 * time.Millisecond),
			Overflow:      "block_with_timeout",
			BlockTimeout:  Duration(100 * time.Millisecond),
		},
		Audit: AuditConfig{
			RetentionDays: 7,
			HashAlgorithm: "sha256",
		},
		Commandments: CommandmentsConfig{File: commandmentsPath},
		Cost: CostConfig{Pricing: map[string]ModelPricing{
			"gpt-4o": {Input: 2.5, Output: 10.0},
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- New(cfg, "").Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("daemon run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("daemon did not stop within timeout")
		}
	})

	waitForIPCReady(t, socketPath, 8*time.Second)
	waitForProxyReady(t, proxyAddr, 8*time.Second)

	return daemonProxyE2ERuntime{socketPath: socketPath, proxyAddr: proxyAddr, caPool: caPool}
}

func sendMITMChatCompletion(t *testing.T, proxyAddr, upstreamURL string, caPool *x509.CertPool) (int, string) {
	t.Helper()

	proxyURL, err := url.Parse("http://" + proxyAddr)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	u, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}

	target := "https://" + u.Host + "/v1/chat/completions"
	resp, err := client.Post(target, "application/json", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("send proxy request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read proxy response body: %v", err)
	}

	return resp.StatusCode, string(body)
}

func waitForAuditAIRequestEvent(t *testing.T, socketPath string, outcome audit.Outcome, timeout time.Duration) *audit.AuditEvent {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		result, callErr := client.Call("audit.query", map[string]any{
			"outcome": string(outcome),
			"limit":   100,
		})
		_ = client.Close()
		if callErr != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		var queryResult audit.QueryResult
		if err := json.Unmarshal(result, &queryResult); err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		for _, evt := range queryResult.Events {
			if evt.ActionType == audit.ActionAIRequest && evt.Outcome == outcome {
				return evt
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for ai_request outcome=%s event", outcome)
	return nil
}

func waitForIPCReady(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := ipc.Dial(socketPath)
		if err == nil {
			_ = client.Close()
			return
		}
		time.Sleep(30 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for ipc socket %s", socketPath)
}

func waitForProxyReady(t *testing.T, proxyAddr string, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + proxyAddr + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for proxy health at %s", proxyAddr)
}

func shortRuntimeDir(t *testing.T, prefix string) string {
	t.Helper()

	base := filepath.Join(os.TempDir(), "cw")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("create short runtime base dir: %v", err)
	}

	dir, err := os.MkdirTemp(base, prefix+"-")
	if err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func freeLocalAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate free tcp addr: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

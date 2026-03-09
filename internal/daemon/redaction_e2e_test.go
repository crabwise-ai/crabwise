package daemon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	proxyadapter "github.com/crabwise-ai/crabwise/internal/adapter/proxy"
	"github.com/crabwise-ai/crabwise/internal/audit"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// bodyCapturingUpstream records the request body for inspection.
type bodyCapturingUpstream struct {
	mu       sync.Mutex
	captured []string
	server   *httptest.Server
}

func newBodyCapturingUpstream() *bodyCapturingUpstream {
	bc := &bodyCapturingUpstream{}
	bc.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bc.mu.Lock()
		bc.captured = append(bc.captured, string(body))
		bc.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-redact-e2e",
			"model": "gpt-4o",
			"choices": []map[string]any{
				{"finish_reason": "stop", "message": map[string]string{"content": "ok"}},
			},
			"usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 3},
		})
	}))
	return bc
}

func (bc *bodyCapturingUpstream) lastBody() string {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.captured) == 0 {
		return ""
	}
	return bc.captured[len(bc.captured)-1]
}

func (bc *bodyCapturingUpstream) Close() { bc.server.Close() }

// startRedactionE2ERuntime starts a full daemon with redaction config.
func startRedactionE2ERuntime(t *testing.T, upstreamURL string, redactDefault bool, redactPatterns []string) daemonProxyE2ERuntime {
	t.Helper()

	runtimeDir := shortRuntimeDir(t, "redact-e2e")
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
	// No commandments (allow all)
	if err := os.WriteFile(commandmentsPath, []byte("version: \"1\"\ncommandments: []\n"), 0o600); err != nil {
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
				Enabled:             true,
				Listen:              proxyAddr,
				DefaultProvider:     "openai",
				UpstreamTimeout:     Duration(3 * time.Second),
				StreamIdleTimeout:   Duration(3 * time.Second),
				MaxRequestBody:      1 << 20,
				RedactEgressDefault: redactDefault,
				RedactPatterns:      redactPatterns,
				CACert:              caCertPath,
				CAKey:               caKeyPath,
				MappingsDir:         mappingsDir,
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

// sendMITMChatCompletionWithBody sends a custom body through the CONNECT+MITM proxy.
func sendMITMChatCompletionWithBody(t *testing.T, proxyAddr, upstreamURL string, caPool *x509.CertPool, body string) (int, string) {
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
	resp, err := client.Post(target, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("send proxy request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read proxy response body: %v", err)
	}

	return resp.StatusCode, string(respBody)
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestDaemonProxyE2E_RedactionApplied(t *testing.T) {
	bc := newBodyCapturingUpstream()
	defer bc.Close()

	upstreamURL := strings.Replace(bc.server.URL, "127.0.0.1", "localhost", 1)
	secret := "sk-" + strings.Repeat("A", 30) // matches sk-[A-Za-z0-9]{20,}

	runtime := startRedactionE2ERuntime(t, upstreamURL, true, []string{`sk-[A-Za-z0-9]{20,}`})

	reqBody := fmt.Sprintf(`{"model":"gpt-4o","messages":[{"role":"user","content":"key is %s"}]}`, secret)
	status, _ := sendMITMChatCompletionWithBody(t, runtime.proxyAddr, upstreamURL, runtime.caPool, reqBody)
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}

	// Verify upstream received redacted body
	upstreamBody := bc.lastBody()
	if strings.Contains(upstreamBody, secret) {
		t.Fatalf("expected upstream body to NOT contain secret, got: %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, "[REDACTED]") {
		t.Fatalf("expected upstream body to contain [REDACTED], got: %s", upstreamBody)
	}

	// Verify audit event has egress_redacted metadata
	event := waitForAuditAIRequestEvent(t, runtime.socketPath, audit.OutcomeSuccess, 10*time.Second)
	if !strings.Contains(event.Arguments, "egress_redacted") {
		t.Fatalf("expected audit event arguments to contain egress_redacted, got: %s", event.Arguments)
	}
}

func TestDaemonProxyE2E_RedactionDisabled(t *testing.T) {
	bc := newBodyCapturingUpstream()
	defer bc.Close()

	upstreamURL := strings.Replace(bc.server.URL, "127.0.0.1", "localhost", 1)
	secret := "sk-" + strings.Repeat("B", 30)

	// redact_egress_default=false, no patterns
	runtime := startRedactionE2ERuntime(t, upstreamURL, false, nil)

	reqBody := fmt.Sprintf(`{"model":"gpt-4o","messages":[{"role":"user","content":"key is %s"}]}`, secret)
	status, _ := sendMITMChatCompletionWithBody(t, runtime.proxyAddr, upstreamURL, runtime.caPool, reqBody)
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}

	// Verify upstream received original body (no redaction)
	upstreamBody := bc.lastBody()
	if !strings.Contains(upstreamBody, secret) {
		t.Fatalf("expected upstream body to contain secret (redaction disabled), got: %s", upstreamBody)
	}

	// Verify audit event does NOT have egress_redacted
	event := waitForAuditAIRequestEvent(t, runtime.socketPath, audit.OutcomeSuccess, 10*time.Second)
	if strings.Contains(event.Arguments, "egress_redacted") {
		t.Fatalf("expected audit event to NOT contain egress_redacted when disabled, got: %s", event.Arguments)
	}
}

func TestDaemonProxyE2E_RedactionBounded(t *testing.T) {
	bc := newBodyCapturingUpstream()
	defer bc.Close()

	upstreamURL := strings.Replace(bc.server.URL, "127.0.0.1", "localhost", 1)

	runtime := startRedactionE2ERuntime(t, upstreamURL, true, []string{`sk-[A-Za-z0-9]{20,}`})

	// Build body with 60 distinct secrets — more than maxRedactionsPerField (50)
	var secrets []string
	for i := 0; i < 60; i++ {
		secrets = append(secrets, fmt.Sprintf("sk-%s%02d", strings.Repeat("C", 20), i))
	}
	content := strings.Join(secrets, " ")
	reqBody := fmt.Sprintf(`{"model":"gpt-4o","messages":[{"role":"user","content":"%s"}]}`, content)

	status, _ := sendMITMChatCompletionWithBody(t, runtime.proxyAddr, upstreamURL, runtime.caPool, reqBody)
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}

	upstreamBody := bc.lastBody()
	redactedCount := strings.Count(upstreamBody, "[REDACTED]")
	// Should be bounded at 50 (maxRedactionsPerField)
	if redactedCount > 50 {
		t.Fatalf("expected at most 50 redactions (bounded), got %d", redactedCount)
	}
	if redactedCount == 0 {
		t.Fatal("expected some redactions, got 0")
	}
}

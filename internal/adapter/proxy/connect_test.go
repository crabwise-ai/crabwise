package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
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

	"github.com/crabwise-ai/crabwise/internal/audit"
)

var testMappingYAML = []byte(`
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
`)

func TestMain(m *testing.M) {
	SetEmbeddedOpenAIMapping(testMappingYAML)
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func generateTestCA(t *testing.T) (certPath, keyPath string, pool *x509.CertPool) {
	t.Helper()
	dir := t.TempDir()
	certPath = filepath.Join(dir, "ca.crt")
	keyPath = filepath.Join(dir, "ca.key")
	if err := GenerateCA(certPath, keyPath); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	pem, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	pool = x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("failed to add test CA to cert pool")
	}
	return
}

func startTestProxy(t *testing.T, cfg Config, eval Evaluator) string {
	return startTestProxyWithEvents(t, cfg, eval, make(chan *audit.AuditEvent, 100))
}

func startTestProxyWithEvents(t *testing.T, cfg Config, eval Evaluator, events chan *audit.AuditEvent) string {
	t.Helper()
	addr := freePort(t)
	cfg.Listen = addr

	p, err := New(cfg, eval, nil, events)
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = p.Stop()
	})

	go p.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return addr
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("proxy at %s not ready", addr)
	return ""
}

func testProxyConfig(upstreamURL, caCert, caKey string) Config {
	return Config{
		DefaultProvider:   "openai",
		UpstreamTimeout:   5 * time.Second,
		StreamIdleTimeout: 5 * time.Second,
		MaxRequestBody:    1 << 20,
		CACert:            caCert,
		CAKey:             caKey,
		Providers: map[string]ProviderConfig{
			"openai": {
				UpstreamBaseURL: upstreamURL,
				AuthMode:        "passthrough",
				RoutePatterns:   []string{"/v1/*"},
			},
		},
	}
}

func TestStartTestProxy_WithNilEventsChannel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	caCert, caKey, caPool := generateTestCA(t)
	cfg := testProxyConfig(upstreamURL, caCert, caKey)
	proxyAddr := startTestProxyWithEvents(t, cfg, allowEval{}, nil)

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	u, _ := url.Parse(upstreamURL)
	target := "https://" + u.Host + "/v1/chat/completions"
	resp, err := client.Post(target, "application/json", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// mock evaluators
// ---------------------------------------------------------------------------

type allowEval struct{}

func (allowEval) Evaluate(_ *audit.AuditEvent) audit.EvalResult {
	return audit.EvalResult{}
}

type blockEval struct{}

func (blockEval) Evaluate(_ *audit.AuditEvent) audit.EvalResult {
	return audit.EvalResult{
		Triggered: []audit.TriggeredRule{
			{Name: "test-block", Enforcement: "block", Message: "blocked by test"},
		},
	}
}

// ---------------------------------------------------------------------------
// integration tests
// ---------------------------------------------------------------------------

func TestConnectMITM_KnownDomain(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-test",
			"model": "gpt-4o",
			"choices": []map[string]any{
				{"finish_reason": "stop", "message": map[string]string{"content": "hello"}},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer upstream.Close()

	// Use "localhost" so the MITM-generated cert (DNSNames) passes TLS verification.
	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)

	caCert, caKey, caPool := generateTestCA(t)
	cfg := testProxyConfig(upstreamURL, caCert, caKey)
	proxyAddr := startTestProxy(t, cfg, allowEval{})

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	u, _ := url.Parse(upstreamURL)
	target := "https://" + u.Host + "/v1/chat/completions"
	resp, err := client.Post(target, "application/json",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("MITM proxy request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["id"] != "chatcmpl-test" {
		t.Fatalf("unexpected response body: %v", result)
	}

	// The peer cert must be signed by the test CA (proves MITM happened).
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		t.Fatal("expected TLS peer certificates from MITM")
	}
	if cn := resp.TLS.PeerCertificates[0].Issuer.CommonName; cn != "Crabwise Local CA" {
		t.Fatalf("expected MITM cert issued by test CA, got issuer CN=%q", cn)
	}
}

func TestConnectTunnel_UnknownDomain(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tunnel": "ok"})
	}))
	defer upstream.Close()

	caCert, caKey, _ := generateTestCA(t)

	// Provider points to an unrelated domain so the test server is "unknown".
	cfg := testProxyConfig("https://api.openai.com", caCert, caKey)
	proxyAddr := startTestProxy(t, cfg, allowEval{})

	// Trust the upstream server's own cert (not our CA).
	serverPool := x509.NewCertPool()
	serverPool.AddCert(upstream.Certificate())

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: serverPool,
			},
		},
	}

	resp, err := client.Get(upstream.URL + "/anything")
	if err != nil {
		t.Fatalf("tunnel request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["tunnel"] != "ok" {
		t.Fatalf("unexpected tunnel response: %v", result)
	}

	// The cert should be the server's real cert, NOT one minted by our CA.
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		t.Fatal("expected TLS peer certificates")
	}
	issuer := resp.TLS.PeerCertificates[0].Issuer
	if len(issuer.Organization) > 0 && issuer.Organization[0] == "Crabwise" {
		t.Fatal("tunnel should use server's real cert, not a MITM cert from test CA")
	}
}

func TestConnectMITM_CommandmentBlocks(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	caCert, caKey, caPool := generateTestCA(t)
	cfg := testProxyConfig(upstreamURL, caCert, caKey)
	proxyAddr := startTestProxy(t, cfg, blockEval{})

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	u, _ := url.Parse(upstreamURL)
	target := "https://" + u.Host + "/v1/chat/completions"
	resp, err := client.Post(target, "application/json",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("blocked request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d", resp.StatusCode)
	}

	var errBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errMap, ok := errBody["error"].(map[string]any); ok {
		if errMap["type"] != "policy_violation" {
			t.Fatalf("expected error type policy_violation, got %v", errMap["type"])
		}
	} else {
		t.Fatalf("expected error object in response, got: %v", errBody)
	}

	if n := upstreamHits.Load(); n != 0 {
		t.Fatalf("upstream should not be hit when blocked; got %d hits", n)
	}
}

func TestConnectMITM_SSEStreaming(t *testing.T) {
	// Fake upstream sends an SSE stream matching the OpenAI streaming format.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected http.Flusher")
			return
		}

		chunks := []string{
			`{"id":"chatcmpl-stream","model":"gpt-4o","choices":[{"delta":{"content":"Hello"}}]}`,
			`{"id":"chatcmpl-stream","model":"gpt-4o","choices":[{"delta":{"content":" world"}}]}`,
			`{"id":"chatcmpl-stream","model":"gpt-4o","choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1)
	caCert, caKey, caPool := generateTestCA(t)
	cfg := testProxyConfig(upstreamURL, caCert, caKey)
	proxyAddr := startTestProxy(t, cfg, allowEval{})

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	u, _ := url.Parse(upstreamURL)
	target := "https://" + u.Host + "/v1/chat/completions"
	resp, err := client.Post(target, "application/json",
		strings.NewReader(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("SSE streaming request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Read entire body and verify we got all SSE chunks intact.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE body: %v", err)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, `"content":"Hello"`) {
		t.Fatalf("missing first SSE chunk in response body")
	}
	if !strings.Contains(bodyStr, `"content":" world"`) {
		t.Fatalf("missing second SSE chunk in response body")
	}
	if !strings.Contains(bodyStr, `"finish_reason":"stop"`) {
		t.Fatalf("missing finish_reason in response body")
	}
	if !strings.Contains(bodyStr, "[DONE]") {
		t.Fatalf("missing [DONE] sentinel in response body")
	}

	// Verify MITM cert was used.
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		t.Fatal("expected TLS peer certificates from MITM")
	}
	if cn := resp.TLS.PeerCertificates[0].Issuer.CommonName; cn != "Crabwise Local CA" {
		t.Fatalf("expected MITM cert, got issuer CN=%q", cn)
	}
}

// ---------------------------------------------------------------------------
// unit tests for CA / CertCache
// ---------------------------------------------------------------------------

func TestCAGeneration_Idempotent(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")

	if err := GenerateCA(certPath, keyPath); err != nil {
		t.Fatalf("first GenerateCA: %v", err)
	}
	if err := GenerateCA(certPath, keyPath); err != nil {
		t.Fatalf("second GenerateCA (overwrite): %v", err)
	}

	ca, err := LoadCA(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadCA after overwrite: %v", err)
	}

	cache := NewCertCache(ca, 8)
	cert, err := cache.GetOrCreate("test.example.com")
	if err != nil {
		t.Fatalf("sign with regenerated CA: %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse generated cert: %v", err)
	}
	if leaf.Subject.CommonName != "test.example.com" {
		t.Fatalf("expected CN=test.example.com, got %s", leaf.Subject.CommonName)
	}
}

func TestCertCache_BoundedSize(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")
	if err := GenerateCA(certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	ca, err := LoadCA(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}

	cache := NewCertCache(ca, 2)

	certA, err := cache.GetOrCreate("a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.GetOrCreate("b.example.com"); err != nil {
		t.Fatal(err)
	}

	// Third insert exceeds maxSize → cache is cleared, then "c" stored.
	certC, err := cache.GetOrCreate("c.example.com")
	if err != nil {
		t.Fatalf("GetOrCreate after eviction: %v", err)
	}

	// "a" was evicted; re-requesting it must produce a fresh cert.
	certA2, err := cache.GetOrCreate("a.example.com")
	if err != nil {
		t.Fatalf("re-request after eviction: %v", err)
	}
	if certA2 == certA {
		t.Fatal("expected new cert instance after eviction, got same pointer")
	}

	for _, tc := range []struct {
		host string
		cert *tls.Certificate
	}{
		{"c.example.com", certC},
		{"a.example.com", certA2},
	} {
		leaf, err := x509.ParseCertificate(tc.cert.Certificate[0])
		if err != nil {
			t.Fatalf("parse cert for %s: %v", tc.host, err)
		}
		if leaf.Subject.CommonName != tc.host {
			t.Fatalf("expected CN=%s, got %s", tc.host, leaf.Subject.CommonName)
		}
	}
}

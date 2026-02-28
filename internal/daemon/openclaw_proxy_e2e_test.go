package daemon

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

	proxyadapter "github.com/crabwise-ai/crabwise/internal/adapter/proxy"
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/gorilla/websocket"
)

func TestOpenClawProxyE2E_AttributedBlockedRequest(t *testing.T) {
	var upstreamHits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	gateway := newOpenClawE2EGateway(t, "gpt-4o")
	defer gateway.Close()

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

	runtime := startDaemonOpenClawProxyE2ERuntime(t, upstreamURL, blockRules, gateway.URL())
	waitForOpenClawConnected(t, runtime.socketPath, 5*time.Second)
	waitForOpenClawObserverEvent(t, runtime.socketPath, 5*time.Second)

	status, body := sendMITMChatCompletion(t, runtime.proxyAddr, upstreamURL, runtime.caPool)
	if status != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d (body=%s)", status, body)
	}
	if hits := upstreamHits.Load(); hits != 0 {
		t.Fatalf("expected upstream hit == 0, got %d", hits)
	}

	event := waitForAuditAIRequestEvent(t, runtime.socketPath, audit.OutcomeBlocked, 10*time.Second)
	if event.AgentID != "openclaw" {
		t.Fatalf("expected attributed openclaw event, got %q", event.AgentID)
	}
	if event.SessionID != "agent:main:discord:channel:123" {
		t.Fatalf("expected session id from OpenClaw correlation, got %q", event.SessionID)
	}
}

func TestOpenClawProxyE2E_GatewayUnavailableStillBlocks(t *testing.T) {
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

	runtime := startDaemonOpenClawProxyE2ERuntime(t, upstreamURL, blockRules, "ws://127.0.0.1:1")

	status, body := sendMITMChatCompletion(t, runtime.proxyAddr, upstreamURL, runtime.caPool)
	if status != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d (body=%s)", status, body)
	}
	if hits := upstreamHits.Load(); hits != 0 {
		t.Fatalf("expected upstream hit == 0, got %d", hits)
	}

	event := waitForAuditAIRequestEvent(t, runtime.socketPath, audit.OutcomeBlocked, 10*time.Second)
	if event.AgentID != "proxy" {
		t.Fatalf("expected unattributed proxy event when gateway unavailable, got %q", event.AgentID)
	}
	if event.SessionID != "" {
		t.Fatalf("expected empty session id when gateway unavailable, got %q", event.SessionID)
	}
}

func startDaemonOpenClawProxyE2ERuntime(t *testing.T, upstreamURL, commandmentsYAML, gatewayURL string) daemonProxyE2ERuntime {
	t.Helper()

	runtimeDir := shortRuntimeDir(t, "openclaw-e2e")
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
			OpenClaw: OpenClawConfig{
				Enabled:                true,
				GatewayURL:             gatewayURL,
				APITokenEnv:            "OPENCLAW_API_TOKEN",
				SessionRefreshInterval: Duration(1 * time.Hour),
				CorrelationWindow:      Duration(3 * time.Second),
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
		errCh <- New(cfg).Run(ctx)
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

func waitForOpenClawConnected(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		result, callErr := client.Call("status", nil)
		_ = client.Close()
		if callErr != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var status map[string]any
		if err := json.Unmarshal(result, &status); err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if status["openclaw_connected"] == float64(1) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for openclaw connected status")
}

func waitForOpenClawObserverEvent(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		result, callErr := client.Call("audit.query", map[string]any{
			"agent": "openclaw",
			"limit": 20,
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
			if evt.AgentID == "openclaw" && evt.Action == "chat" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for OpenClaw observer chat event")
}

type openClawE2EGateway struct {
	url      string
	listener net.Listener
	server   *http.Server
}

func newOpenClawE2EGateway(t *testing.T, model string) *openClawE2EGateway {
	t.Helper()

	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		go func() {
			defer conn.Close()
			if err := conn.WriteJSON(map[string]any{
				"type":     "hello-ok",
				"protocol": 3,
				"snapshot": map[string]any{
					"presence": []any{},
					"health":   map[string]any{},
					"stateVersion": map[string]any{
						"presence": 1,
						"health":   1,
					},
				},
				"features": map[string]any{
					"methods": []string{"sessions.list"},
					"events":  []string{"chat"},
				},
			}); err != nil {
				return
			}

			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("unmarshal gateway request: %v", err)
				return
			}

			if err := conn.WriteJSON(map[string]any{
				"type": "res",
				"id":   req.ID,
				"ok":   true,
				"payload": map[string]any{
					"sessions": []map[string]any{
						{
							"key":            "agent:main:discord:channel:123",
							"agentId":        "main",
							"createdAt":      time.Now().Add(-time.Minute).UnixMilli(),
							"lastActivityAt": time.Now().UnixMilli(),
							"messageCount":   2,
							"model":          model,
						},
					},
				},
			}); err != nil {
				return
			}

			time.Sleep(100 * time.Millisecond)
			_ = conn.WriteJSON(map[string]any{
				"type":  "event",
				"event": "chat",
				"payload": map[string]any{
					"runId":      "run-1",
					"sessionKey": "agent:main:discord:channel:123",
					"seq":        1,
					"state":      "final",
				},
			})
			<-r.Context().Done()
		}()
	})

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp4: %v", err)
	}

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(listener)
	}()

	return &openClawE2EGateway{
		url:      fmt.Sprintf("ws://%s/", listener.Addr().String()),
		listener: listener,
		server:   srv,
	}
}

func (g *openClawE2EGateway) URL() string {
	return g.url
}

func (g *openClawE2EGateway) Close() {
	_ = g.server.Close()
	_ = g.listener.Close()
}

func sendOpenClawMITMChatCompletion(t *testing.T, proxyAddr, upstreamURL string, caPool *x509.CertPool) (int, string) {
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

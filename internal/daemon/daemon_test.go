package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/discovery"
	"github.com/crabwise-ai/crabwise/internal/openclawstate"
	"github.com/gorilla/websocket"
)

func TestReloadRuntime_ReturnsCombinedErrorWhenBothReloadsFail(t *testing.T) {
	dir := t.TempDir()

	invalidCommandmentsPath := filepath.Join(dir, "commandments-invalid.yaml")
	if err := os.WriteFile(invalidCommandmentsPath, []byte("rules:\n  - :"), 0600); err != nil {
		t.Fatalf("write invalid commandments: %v", err)
	}

	invalidRegistryPath := filepath.Join(dir, "registry-invalid.yaml")
	if err := os.WriteFile(invalidRegistryPath, []byte("providers:\n  openai:\n    tools:\n      bad: ["), 0600); err != nil {
		t.Fatalf("write invalid registry: %v", err)
	}

	d := &Daemon{
		cfg: &Config{
			Commandments: CommandmentsConfig{File: invalidCommandmentsPath},
			ToolRegistry: ToolRegistryConfig{File: invalidRegistryPath},
		},
	}

	_, err := d.reloadRuntime()
	if err == nil {
		t.Fatal("expected combined runtime reload error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "reload commandments") {
		t.Fatalf("expected commandments error context, got: %s", msg)
	}
	if !strings.Contains(msg, "reload tool registry") {
		t.Fatalf("expected tool registry error context, got: %s", msg)
	}
}

func TestDaemonStatusIncludesOpenClaw(t *testing.T) {
	now := time.Now()
	d := &Daemon{
		registry:      discovery.NewRegistry(),
		openclawState: openclawstate.New(3 * time.Second),
	}
	d.registry.ReplaceSource("openclaw-gateway", []discovery.AgentInfo{
		{ID: "openclaw/agent:main:discord:channel:123", Type: "openclaw", LastActivityAt: now},
	})
	d.openclawState.RecordChat("run-1", "agent:main:discord:channel:123", "anthropic", "claude-sonnet", now)
	d.openclawState.RecordSession(openclawstate.SessionMeta{
		SessionKey:    "agent:main:discord:channel:123",
		AgentID:       "main",
		Model:         "claude-sonnet",
		ThinkingLevel: "high",
	})
	d.openclawState.MatchProxyRequest(now, "anthropic", "claude-sonnet")
	d.openclawState.RecordSession(openclawstate.SessionMeta{SessionKey: "agent:main:discord:channel:456", AgentID: "main"})
	d.openclawState.RecordChat("run-2", "agent:main:discord:channel:456", "anthropic", "claude-sonnet", now)
	d.openclawState.MatchProxyRequest(now, "anthropic", "claude-sonnet")

	status := d.statusSnapshot()
	if status["openclaw_connected"] != 0.0 {
		t.Fatalf("expected disconnected OpenClaw adapter by default, got %#v", status["openclaw_connected"])
	}
	if status["openclaw_session_cache_size"] != float64(2) {
		t.Fatalf("expected session cache size 2, got %#v", status["openclaw_session_cache_size"])
	}
	if status["openclaw_correlation_matches"] != float64(1) {
		t.Fatalf("expected 1 correlation match, got %#v", status["openclaw_correlation_matches"])
	}
	if status["openclaw_correlation_ambiguous"] != float64(1) {
		t.Fatalf("expected 1 ambiguous correlation, got %#v", status["openclaw_correlation_ambiguous"])
	}
}

func TestDaemonStartsOpenClawAdapter(t *testing.T) {
	server := newDaemonFakeGatewayServer(t, func(conn *websocket.Conn) {
		writeDaemonGatewayJSON(t, conn, map[string]interface{}{
			"type":     "hello-ok",
			"protocol": 3,
			"snapshot": map[string]interface{}{
				"presence": []interface{}{},
				"health":   map[string]interface{}{},
				"stateVersion": map[string]interface{}{
					"presence": 1,
					"health":   1,
				},
			},
			"features": map[string]interface{}{
				"methods": []string{"sessions.list"},
				"events":  []string{"chat"},
			},
		})

		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}

		writeDaemonGatewayJSON(t, conn, map[string]interface{}{
			"type": "res",
			"id":   req.ID,
			"ok":   true,
			"payload": map[string]interface{}{
				"sessions": []map[string]interface{}{
					{
						"key":            "agent:main:discord:channel:123",
						"agentId":        "main",
						"createdAt":      1730000000000,
						"lastActivityAt": time.Now().UnixMilli(),
						"messageCount":   2,
						"model":          "claude-sonnet",
					},
				},
			},
		})
	})
	defer server.Close()

	d := &Daemon{
		cfg: &Config{
			Adapters: AdaptersConfig{
				OpenClaw: OpenClawConfig{
					Enabled:                true,
					GatewayURL:             server.URL(),
					APITokenEnv:            "OPENCLAW_API_TOKEN",
					SessionRefreshInterval: Duration(time.Hour),
					CorrelationWindow:      Duration(3 * time.Second),
				},
			},
		},
		registry: discovery.NewRegistry(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.startOpenClaw(ctx); err != nil {
		t.Fatalf("start openclaw: %v", err)
	}
	defer func() {
		if d.openclaw != nil {
			_ = d.openclaw.Stop()
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := d.registry.Get("openclaw/agent:main:discord:channel:123"); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected openclaw session to be registered")
}

func newDaemonFakeGatewayServer(t *testing.T, onConnect func(conn *websocket.Conn)) *daemonFakeGatewayServer {
	t.Helper()

	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		go onConnect(conn)
	})

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp4: %v", err)
	}

	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(listener)
	}()

	return &daemonFakeGatewayServer{
		url:      fmt.Sprintf("ws://%s/", listener.Addr().String()),
		listener: listener,
		server:   srv,
	}
}

type daemonFakeGatewayServer struct {
	url      string
	listener net.Listener
	server   *http.Server
}

func (s *daemonFakeGatewayServer) URL() string {
	return s.url
}

func (s *daemonFakeGatewayServer) Close() {
	_ = s.server.Close()
	_ = s.listener.Close()
}

func writeDaemonGatewayJSON(t *testing.T, conn *websocket.Conn, payload interface{}) {
	t.Helper()
	if err := conn.WriteJSON(payload); err != nil {
		t.Fatalf("write websocket json: %v", err)
	}
}

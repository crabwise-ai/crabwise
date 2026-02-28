package openclaw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestGatewayClientConnect(t *testing.T) {
	t.Parallel()

	server := newFakeGatewayServer(t, func(conn *websocket.Conn) {
		writeGatewayJSON(t, conn, map[string]interface{}{
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

		writeGatewayJSON(t, conn, map[string]interface{}{
			"type":  "event",
			"event": "chat",
			"payload": map[string]interface{}{
				"runId":      "run-1",
				"sessionKey": "agent:main:discord:channel:123",
				"seq":        1,
				"state":      "final",
			},
		})
	})
	defer server.Close()

	client := NewGatewayClient(Config{GatewayURL: server.URL()})
	defer client.Close()

	events := make(chan *EventFrame, 1)
	client.OnEvent(func(evt *EventFrame) {
		events <- evt
	})

	hello, err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if hello.Protocol != 3 {
		t.Fatalf("expected protocol 3, got %d", hello.Protocol)
	}
	if !client.Connected() {
		t.Fatal("expected client to report connected")
	}

	select {
	case evt := <-events:
		if evt.Event != "chat" {
			t.Fatalf("expected chat event, got %q", evt.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for chat event")
	}
}

func TestGatewayClientConnect_AuthChallengeRequiresToken(t *testing.T) {
	t.Parallel()

	server := newFakeGatewayServer(t, func(conn *websocket.Conn) {
		writeGatewayJSON(t, conn, map[string]interface{}{
			"type":  "event",
			"event": "connect.challenge",
			"payload": map[string]interface{}{
				"nonce": "nonce",
				"ts":    time.Now().UnixMilli(),
			},
		})
		_ = conn.Close()
	})
	defer server.Close()

	client := NewGatewayClient(Config{GatewayURL: server.URL()})
	defer client.Close()

	if _, err := client.Connect(context.Background()); err == nil {
		t.Fatal("expected auth challenge connection to fail without API token")
	}
}

func TestGatewayClientListSessions(t *testing.T) {
	t.Parallel()

	server := newFakeGatewayServer(t, func(conn *websocket.Conn) {
		writeGatewayJSON(t, conn, map[string]interface{}{
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
		var req RequestFrame
		if err := json.Unmarshal(data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}

		writeGatewayJSON(t, conn, map[string]interface{}{
			"type": "res",
			"id":   req.ID,
			"ok":   true,
			"payload": map[string]interface{}{
				"sessions": []map[string]interface{}{
					{
						"key":            "agent:main:discord:channel:123",
						"agentId":        "main",
						"createdAt":      1730000000000,
						"lastActivityAt": 1730000001000,
						"messageCount":   2,
						"model":          "claude-sonnet",
					},
				},
			},
		})
	})
	defer server.Close()

	client := NewGatewayClient(Config{GatewayURL: server.URL()})
	defer client.Close()

	if _, err := client.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	sessions, err := client.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].AgentID != "main" {
		t.Fatalf("expected agent id main, got %q", sessions[0].AgentID)
	}
}

type fakeGatewayServer struct {
	url      string
	listener net.Listener
	server   *http.Server
}

func newFakeGatewayServer(t *testing.T, onConnect func(conn *websocket.Conn)) *fakeGatewayServer {
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

	return &fakeGatewayServer{
		url:      fmt.Sprintf("ws://%s/", listener.Addr().String()),
		listener: listener,
		server:   srv,
	}
}

func (s *fakeGatewayServer) URL() string {
	return s.url
}

func (s *fakeGatewayServer) Close() {
	_ = s.server.Close()
	_ = s.listener.Close()
}

func writeGatewayJSON(t *testing.T, conn *websocket.Conn, payload interface{}) {
	t.Helper()
	if err := conn.WriteJSON(payload); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("write websocket json: %v", err)
	}
}


package openclaw

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/openclawstate"
	"github.com/gorilla/websocket"
)

func TestAdapterStart(t *testing.T) {
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
				"events":  []string{"chat", "agent"},
			},
		})

		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
		writeGatewayJSON(t, conn, map[string]interface{}{
			"type": "res",
			"id":   "req-1",
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
						"thinkingLevel":  "high",
					},
				},
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
		writeGatewayJSON(t, conn, map[string]interface{}{
			"type":  "event",
			"event": "agent",
			"payload": map[string]interface{}{
				"runId":      "run-1",
				"seq":        2,
				"stream":     "stdout",
				"ts":         time.Now().UnixMilli(),
				"sessionKey": "agent:main:discord:channel:123",
				"data": map[string]interface{}{
					"line": "agent output",
				},
			},
		})
	})
	defer server.Close()

	state := openclawstate.New(3 * time.Second)
	adapter := NewAdapter(Config{
		GatewayURL:             server.URL(),
		SessionRefreshInterval: time.Hour,
		CorrelationWindow:      3 * time.Second,
	}, state)

	events := make(chan *audit.AuditEvent, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Start(ctx, events); err != nil {
		t.Fatalf("start adapter: %v", err)
	}
	defer adapter.Stop()

	evt := waitForEvent(t, events, func(evt *audit.AuditEvent) bool {
		return evt.Action == "chat"
	})
	if evt.AgentID != "openclaw" {
		t.Fatalf("expected agent id openclaw, got %q", evt.AgentID)
	}
	if evt.ActionType != audit.ActionAIRequest {
		t.Fatalf("expected ai_request action type, got %q", evt.ActionType)
	}
	if evt.AdapterID != "openclaw-gateway" {
		t.Fatalf("expected adapter id openclaw-gateway, got %q", evt.AdapterID)
	}
	if evt.AdapterType != "gateway_observer" {
		t.Fatalf("expected adapter type gateway_observer, got %q", evt.AdapterType)
	}
	if evt.Arguments == "" || !containsJSONField(evt.Arguments, "openclaw.correlation_confidence", "observer") {
		t.Fatalf("expected observer correlation confidence in arguments, got %q", evt.Arguments)
	}

	match, ok := state.MatchProxyRequest(time.Now(), "anthropic", "claude-sonnet")
	if !ok {
		t.Fatal("expected correlation state to match proxied request")
	}
	if match.SessionKey != "agent:main:discord:channel:123" {
		t.Fatalf("expected correlated session key, got %q", match.SessionKey)
	}

	agentEvt := waitForEvent(t, events, func(evt *audit.AuditEvent) bool {
		return evt.Action == "agent"
	})
	if agentEvt.AdapterType != "gateway_observer" {
		t.Fatalf("expected adapter type gateway_observer, got %q", agentEvt.AdapterType)
	}
}

func TestAdapterCanEnforce(t *testing.T) {
	adapter := NewAdapter(Config{}, openclawstate.New(3*time.Second))
	if adapter.CanEnforce() {
		t.Fatal("expected openclaw adapter to be read-only")
	}
}

func containsJSONField(raw, key, want string) bool {
	return strings.Contains(raw, `"`+key+`":"`+want+`"`)
}

func waitForEvent(t *testing.T, events <-chan *audit.AuditEvent, match func(*audit.AuditEvent) bool) *audit.AuditEvent {
	t.Helper()

	timeout := time.After(2 * time.Second)
	for {
		select {
		case evt := <-events:
			if match(evt) {
				return evt
			}
		case <-timeout:
			t.Fatal("timed out waiting for expected event")
		}
	}
}

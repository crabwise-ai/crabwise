package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestWebhookBackend_SendPayload(t *testing.T) {
	var received webhookPayload
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	const envKey = "TEST_CRABWISE_WEBHOOK_AUTH"
	os.Setenv(envKey, "Bearer test-token")
	defer os.Unsetenv(envKey)

	b := NewWebhookBackend(WebhookConfig{
		Enabled:       true,
		URL:           srv.URL,
		AuthHeaderEnv: envKey,
	})

	evt := &audit.AuditEvent{
		ID:                    "evt1",
		Timestamp:             time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		AgentID:               "claude",
		Action:                "rm_rf",
		Outcome:               audit.OutcomeBlocked,
		CommandmentsTriggered: `[{"name":"no-destructive","enforcement":"block","message":"blocked destructive cmd"}]`,
	}

	err := b.Send(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.Event != "block" {
		t.Errorf("expected event=block, got %q", received.Event)
	}
	if received.Agent != "claude" {
		t.Errorf("expected agent=claude, got %q", received.Agent)
	}
	if received.Action != "rm_rf" {
		t.Errorf("expected action=rm_rf, got %q", received.Action)
	}
	if received.Rule != "no-destructive" {
		t.Errorf("expected rule=no-destructive, got %q", received.Rule)
	}
	if received.Message != "blocked destructive cmd" {
		t.Errorf("expected message, got %q", received.Message)
	}
	if authHeader != "Bearer test-token" {
		t.Errorf("expected auth header 'Bearer test-token', got %q", authHeader)
	}
}

func TestWebhookBackend_MinInterval(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(200)
	}))
	defer srv.Close()

	b := NewWebhookBackend(WebhookConfig{
		Enabled:     true,
		URL:         srv.URL,
		MinInterval: 1 * time.Hour,
	})

	evt := &audit.AuditEvent{
		ID:        "evt1",
		Timestamp: time.Now().UTC(),
		AgentID:   "test",
		Action:    "test",
		Outcome:   audit.OutcomeBlocked,
	}

	ctx := context.Background()
	_ = b.Send(ctx, evt)
	_ = b.Send(ctx, evt) // should be debounced

	if callCount != 1 {
		t.Errorf("expected 1 webhook call (debounced), got %d", callCount)
	}
}

func TestWebhookBackend_FailureDoesNotConsumeWindow(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	b := NewWebhookBackend(WebhookConfig{
		Enabled:     true,
		URL:         srv.URL,
		MinInterval: 1 * time.Hour,
	})

	evt := &audit.AuditEvent{
		ID:        "evt1",
		Timestamp: time.Now().UTC(),
		AgentID:   "test",
		Action:    "test",
		Outcome:   audit.OutcomeBlocked,
	}

	ctx := context.Background()
	_ = b.Send(ctx, evt) // 500 — should NOT consume debounce window
	_ = b.Send(ctx, evt) // should retry since previous failed

	if callCount != 2 {
		t.Errorf("expected 2 webhook calls (failure should not debounce), got %d", callCount)
	}
}

func TestWebhookBackend_NoAuthWhenEnvEmpty(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	b := NewWebhookBackend(WebhookConfig{
		Enabled: true,
		URL:     srv.URL,
	})

	evt := &audit.AuditEvent{
		ID:        "evt1",
		Timestamp: time.Now().UTC(),
		AgentID:   "test",
		Action:    "test",
		Outcome:   audit.OutcomeBlocked,
	}

	_ = b.Send(context.Background(), evt)

	if authHeader != "" {
		t.Errorf("expected no auth header, got %q", authHeader)
	}
}

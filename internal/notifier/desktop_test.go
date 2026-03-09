package notifier

import (
	"context"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestDesktopBackend_MinInterval(t *testing.T) {
	b := NewDesktopBackend(DesktopConfig{
		Enabled:     true,
		MinInterval: 1 * time.Hour, // very long so second call is debounced
	})

	evt := &audit.AuditEvent{
		ID:      "test1",
		AgentID: "agent1",
		Action:  "write_file",
		Outcome: audit.OutcomeBlocked,
	}

	ctx := context.Background()

	// First call — will attempt to send (may fail if notify-send not installed, that's ok)
	_ = b.Send(ctx, evt)

	// Mark lastSent manually to simulate successful send
	b.mu.Lock()
	b.lastSent = time.Now()
	b.mu.Unlock()

	// Second call should be debounced (no error, just skipped)
	err := b.Send(ctx, evt)
	if err != nil {
		t.Errorf("expected nil error for debounced send, got: %v", err)
	}
}

func TestDesktopBackend_Name(t *testing.T) {
	b := &DesktopBackend{}
	if b.Name() != "desktop" {
		t.Errorf("expected 'desktop', got %q", b.Name())
	}
}

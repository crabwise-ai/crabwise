package cli

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestRunWatchText_IntentionalCloseReturnsNil(t *testing.T) {
	err := watchTextExitErr(true, net.ErrClosed)
	if err != nil {
		t.Fatalf("expected nil error for intentional close, got %v", err)
	}
}

func TestRunWatchText_UnexpectedErrorReturnsError(t *testing.T) {
	want := errors.New("boom")
	err := watchTextExitErr(false, want)
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestFormatWatchTextEvent_ShowsOpenClawSession(t *testing.T) {
	line := formatWatchTextEvent(audit.AuditEvent{
		Timestamp:  time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC),
		AgentID:    "openclaw",
		SessionID:  "agent:main:discord:channel:123",
		ActionType: audit.ActionAIRequest,
		Action:     "chat",
		Arguments:  `{"openclaw.run_id":"run-1"}`,
	})

	if !strings.Contains(line, "[openclaw agent:main:discord:channel:123]") {
		t.Fatalf("expected full openclaw session label, got %q", line)
	}
}

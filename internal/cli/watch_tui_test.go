package cli

import (
	"errors"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestWatchModel_UpdatesCountersOnAuditEvent(t *testing.T) {
	now := time.Now().UTC()
	m := newWatchModel(watchModelDeps{Now: func() time.Time { return now }})

	updated, cmd := m.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "codex",
		ActionType: audit.ActionToolCall,
		Action:     "Bash",
		Arguments:  "{\"command\":\"ls\"}",
	}})
	if cmd != nil {
		t.Fatalf("expected nil cmd for audit event, got %T", cmd)
	}

	next := updated.(watchModel)
	if len(next.feed) != 1 {
		t.Fatalf("expected 1 feed row, got %d", len(next.feed))
	}
	if next.triggersLastMinute != 1 {
		t.Fatalf("expected trigger count 1, got %d", next.triggersLastMinute)
	}
}

func TestWatchModel_ReconnectAttemptOnce(t *testing.T) {
	reconnectCalls := 0
	m := newWatchModel(watchModelDeps{
		Now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
		Reconnect: func() tea.Msg {
			reconnectCalls++
			return reconnectResultMsg{Err: errors.New("dial failed")}
		},
	})

	updated, cmd := m.Update(streamDisconnectedMsg{Err: io.EOF})
	if cmd == nil {
		t.Fatal("expected reconnect scheduling cmd")
	}
	next := updated.(watchModel)
	if next.reconnectAttempts != 1 {
		t.Fatalf("expected one reconnect attempt scheduled, got %d", next.reconnectAttempts)
	}

	updated, cmd = next.Update(reconnectMsg{})
	next = updated.(watchModel)
	if cmd == nil {
		t.Fatal("expected reconnect command")
	}
	updated, _ = next.Update(cmd())
	next = updated.(watchModel)
	if reconnectCalls != 1 {
		t.Fatalf("expected one reconnect call, got %d", reconnectCalls)
	}

	updated, cmd = next.Update(streamDisconnectedMsg{Err: io.EOF})
	if cmd != nil {
		t.Fatalf("expected no cmd after retry exhausted, got %T", cmd)
	}
	next = updated.(watchModel)
	if next.fatalErr == nil {
		t.Fatal("expected fatal error after reconnect attempt is exhausted")
	}
}

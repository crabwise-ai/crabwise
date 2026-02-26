package cli

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestWatchModel_TracksTriggersOnlyForTriggeredEvents(t *testing.T) {
	now := time.Now().UTC()
	m := newWatchModel(watchModelDeps{Now: func() time.Time { return now }})

	updated, cmd := m.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "codex",
		ActionType: audit.ActionToolCall,
		Action:     "Bash",
		Arguments:  "{\"command\":\"ls\"}",
		Outcome:    audit.OutcomeSuccess,
	}})
	if cmd != nil {
		t.Fatalf("expected nil cmd for audit event, got %T", cmd)
	}

	next := updated.(watchModel)
	if len(next.feed) != 1 {
		t.Fatalf("expected 1 feed row, got %d", len(next.feed))
	}
	if next.triggersLastMinute != 0 {
		t.Fatalf("expected trigger count 0 for normal event, got %d", next.triggersLastMinute)
	}

	updated, cmd = next.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:             now,
		AgentID:               "codex",
		ActionType:            audit.ActionToolCall,
		Action:                "Bash",
		Arguments:             "{\"command\":\"ls\"}",
		Outcome:               audit.OutcomeWarned,
		CommandmentsTriggered: `[{"id":"protect-credentials","severity":"high","message":"blocked by policy","enforcement":"warn"}]`,
	}})
	if cmd != nil {
		t.Fatalf("expected nil cmd for audit event, got %T", cmd)
	}

	next = updated.(watchModel)
	if len(next.feed) != 2 {
		t.Fatalf("expected 2 feed rows, got %d", len(next.feed))
	}
	if next.triggersLastMinute != 1 {
		t.Fatalf("expected trigger count 1, got %d", next.triggersLastMinute)
	}
}

func TestWatchModel_StatusPollUpdatesStrip(t *testing.T) {
	now := time.Now().UTC()
	pollCalls := 0
	m := newWatchModel(watchModelDeps{
		Now: func() time.Time { return now },
		PollStatus: func() tea.Msg {
			pollCalls++
			return statusResultMsg{
				QueueDepth:   7,
				QueueDropped: 42,
				Uptime:       "5m30s",
			}
		},
	})

	// Simulate a status tick
	updated, cmd := m.Update(statusTickMsg{})
	if cmd == nil {
		t.Fatal("expected batch cmd from status tick")
	}
	next := updated.(watchModel)

	// Status hasn't been applied yet (tick schedules the poll)
	// Execute the poll result
	updated, _ = next.Update(statusResultMsg{
		QueueDepth:   7,
		QueueDropped: 42,
		Uptime:       "5m30s",
	})
	next = updated.(watchModel)

	if next.queueDepth != 7 {
		t.Fatalf("expected queue depth 7, got %d", next.queueDepth)
	}
	if next.queueDropped != 42 {
		t.Fatalf("expected queue dropped 42, got %d", next.queueDropped)
	}
	if next.daemonUptime != "5m30s" {
		t.Fatalf("expected daemon uptime 5m30s, got %q", next.daemonUptime)
	}

	// View should show daemon uptime, not TUI-local uptime
	view := next.View()
	if !strings.Contains(view, "5m30s") {
		t.Fatalf("expected daemon uptime in view, got: %s", view)
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
	if cmd == nil {
		t.Fatal("expected quit cmd after retry exhausted")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected quit message after retry exhausted, got %T", cmd())
	}
	next = updated.(watchModel)
	if next.fatalErr == nil {
		t.Fatal("expected fatal error after reconnect attempt is exhausted")
	}
}

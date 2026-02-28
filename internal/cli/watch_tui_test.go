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
		OK:           true,
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

func TestWatchModel_StatusPollFailurePreservesMetrics(t *testing.T) {
	now := time.Now().UTC()
	m := newWatchModel(watchModelDeps{Now: func() time.Time { return now }})

	// Apply a successful status first
	updated, _ := m.Update(statusResultMsg{
		OK:           true,
		QueueDepth:   5,
		QueueDropped: 10,
		Uptime:       "2m0s",
	})
	next := updated.(watchModel)

	// Now simulate a failed poll (OK: false)
	updated, _ = next.Update(statusResultMsg{OK: false})
	next = updated.(watchModel)

	if next.queueDepth != 5 {
		t.Fatalf("expected queue depth preserved at 5, got %d", next.queueDepth)
	}
	if next.queueDropped != 10 {
		t.Fatalf("expected queue dropped preserved at 10, got %d", next.queueDropped)
	}
	if next.daemonUptime != "2m0s" {
		t.Fatalf("expected daemon uptime preserved at 2m0s, got %q", next.daemonUptime)
	}
}

func TestWatchModel_FilterMatchesEvents(t *testing.T) {
	now := time.Now().UTC()
	m := newWatchModel(watchModelDeps{Now: func() time.Time { return now }})

	// Add events from two different agents
	updated, _ := m.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "claude",
		ActionType: audit.ActionToolCall,
		Action:     "Bash",
		Arguments:  `{"command":"ls"}`,
		Outcome:    audit.OutcomeSuccess,
	}})
	next := updated.(watchModel)

	updated, _ = next.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "codex",
		ActionType: audit.ActionAIRequest,
		Action:     "chat",
		Arguments:  `{"model":"gpt-4o"}`,
		Outcome:    audit.OutcomeSuccess,
	}})
	next = updated.(watchModel)

	if len(next.feed) != 2 {
		t.Fatalf("expected 2 feed rows, got %d", len(next.feed))
	}

	// Apply filter for "claude"
	next.filterText = "claude"
	next.rebuildFeed()

	if len(next.feed) != 1 {
		t.Fatalf("expected 1 filtered row, got %d", len(next.feed))
	}
	if !strings.Contains(next.feed[0], "claude") {
		t.Fatalf("expected filtered feed to contain 'claude', got: %s", next.feed[0])
	}

	// Clear filter
	next.filterText = ""
	next.rebuildFeed()

	if len(next.feed) != 2 {
		t.Fatalf("expected 2 feed rows after clear, got %d", len(next.feed))
	}
}

func TestWatchModel_FilterMatchesOutcomeKeywords(t *testing.T) {
	now := time.Now().UTC()
	m := newWatchModel(watchModelDeps{Now: func() time.Time { return now }})

	updated, _ := m.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "claude",
		ActionType: audit.ActionToolCall,
		Action:     "Bash",
		Outcome:    audit.OutcomeSuccess,
	}})
	next := updated.(watchModel)

	updated, _ = next.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:             now,
		AgentID:               "codex",
		ActionType:            audit.ActionToolCall,
		Action:                "Write",
		Outcome:               audit.OutcomeWarned,
		CommandmentsTriggered: `[{"id":"x"}]`,
	}})
	next = updated.(watchModel)

	updated, _ = next.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "cursor",
		ActionType: audit.ActionToolCall,
		Action:     "Read",
		Outcome:    audit.OutcomeBlocked,
	}})
	next = updated.(watchModel)

	next.filterText = "blocked"
	next.rebuildFeed()
	if len(next.feed) != 1 {
		t.Fatalf("expected 1 blocked row, got %d", len(next.feed))
	}
	if !strings.Contains(next.feed[0], "cursor") {
		t.Fatalf("expected blocked row for cursor, got: %s", next.feed[0])
	}

	next.filterText = "warned"
	next.rebuildFeed()
	if len(next.feed) != 1 {
		t.Fatalf("expected 1 warned row, got %d", len(next.feed))
	}
	if !strings.Contains(next.feed[0], "codex") {
		t.Fatalf("expected warned row for codex, got: %s", next.feed[0])
	}

	next.filterText = "success"
	next.rebuildFeed()
	if len(next.feed) != 1 {
		t.Fatalf("expected 1 success row, got %d", len(next.feed))
	}
	if !strings.Contains(next.feed[0], "claude") {
		t.Fatalf("expected success row for claude, got: %s", next.feed[0])
	}
}

func TestWatchModel_OpenClawSessionIsSearchableAndVisible(t *testing.T) {
	now := time.Now().UTC()
	m := newWatchModel(watchModelDeps{Now: func() time.Time { return now }})

	updated, _ := m.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "openclaw",
		SessionID:  "agent:main:discord:channel:123",
		ActionType: audit.ActionAIRequest,
		Action:     "chat",
		Arguments:  `{"openclaw.run_id":"run-1"}`,
		Outcome:    audit.OutcomeBlocked,
	}})
	next := updated.(watchModel)

	if len(next.feed) != 1 {
		t.Fatalf("expected 1 feed row, got %d", len(next.feed))
	}
	if !strings.Contains(next.feed[0], "openclaw/main:discord:123") {
		t.Fatalf("expected compact openclaw session label in feed, got %s", next.feed[0])
	}

	next.filterText = "channel:123"
	next.rebuildFeed()
	if len(next.feed) != 1 {
		t.Fatalf("expected session filter to match row, got %d rows", len(next.feed))
	}
}

func TestWatchModel_FilterIndicatorInView(t *testing.T) {
	now := time.Now().UTC()
	m := newWatchModel(watchModelDeps{Now: func() time.Time { return now }})
	m.filterText = "claude"

	view := m.View()
	if !strings.Contains(view, "Filter: claude") {
		t.Fatalf("expected filter indicator in view, got: %s", view)
	}
}

func TestWatchModel_WarnBlockIndicators(t *testing.T) {
	now := time.Now().UTC()
	m := newWatchModel(watchModelDeps{Now: func() time.Time { return now }})

	// Success event - no prefix
	updated, _ := m.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "claude",
		ActionType: audit.ActionToolCall,
		Action:     "Bash",
		Outcome:    audit.OutcomeSuccess,
	}})
	next := updated.(watchModel)

	// Warned event - ⚠ prefix
	updated, _ = next.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:             now,
		AgentID:               "claude",
		ActionType:            audit.ActionToolCall,
		Action:                "Write",
		Outcome:               audit.OutcomeWarned,
		CommandmentsTriggered: `[{"id":"x"}]`,
	}})
	next = updated.(watchModel)

	// Blocked event - ✖ prefix
	updated, _ = next.Update(auditEventMsg{Event: audit.AuditEvent{
		Timestamp:  now,
		AgentID:    "codex",
		ActionType: audit.ActionToolCall,
		Action:     "Bash",
		Outcome:    audit.OutcomeBlocked,
	}})
	next = updated.(watchModel)

	// Feed should have 3 entries (most recent first)
	if len(next.feed) != 3 {
		t.Fatalf("expected 3 feed rows, got %d", len(next.feed))
	}

	// Most recent (blocked) should contain ✖
	if !strings.Contains(next.feed[0], "✖") {
		t.Fatalf("expected blocked event to contain ✖, got: %s", next.feed[0])
	}
	// Warned should contain ⚠
	if !strings.Contains(next.feed[1], "⚠") {
		t.Fatalf("expected warned event to contain ⚠, got: %s", next.feed[1])
	}
	// Success should NOT contain ⚠ or ✖
	if strings.Contains(next.feed[2], "⚠") || strings.Contains(next.feed[2], "✖") {
		t.Fatalf("expected success event to have no indicator, got: %s", next.feed[2])
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

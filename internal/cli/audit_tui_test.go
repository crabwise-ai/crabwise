package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestAuditTUIModel_EventsLoaded(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})

	events := []*audit.AuditEvent{
		{
			Timestamp:  time.Date(2026, 2, 26, 14, 32, 1, 0, time.UTC),
			AgentID:    "claude-a8f2",
			ActionType: audit.ActionToolCall,
			Action:     "Bash",
			Outcome:    audit.OutcomeSuccess,
		},
		{
			Timestamp:  time.Date(2026, 2, 26, 14, 31, 58, 0, time.UTC),
			AgentID:    "claude-a8f2",
			ActionType: audit.ActionAIRequest,
			Action:     "chat",
			Outcome:    audit.OutcomeSuccess,
		},
		{
			Timestamp:  time.Date(2026, 2, 26, 14, 31, 30, 0, time.UTC),
			AgentID:    "claude-a8f2",
			ActionType: audit.ActionCommandExecution,
			Action:     "rm -rf",
			Outcome:    audit.OutcomeBlocked,
		},
	}

	msg := auditEventsLoadedMsg{events: events, total: 47}
	updated, cmd := m.Update(msg)
	if cmd != nil {
		t.Fatalf("expected nil cmd from events loaded, got %T", cmd)
	}
	next := updated.(auditTUIModel)

	if len(next.events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(next.events))
	}
	if next.total != 47 {
		t.Fatalf("expected total 47, got %d", next.total)
	}

	rows := next.table.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 table rows, got %d", len(rows))
	}

	// Verify time column
	if rows[0][0] != "14:32:01" {
		t.Fatalf("expected time '14:32:01', got %q", rows[0][0])
	}

	// Verify agent column
	if rows[0][1] != "claude-a8f2" {
		t.Fatalf("expected agent 'claude-a8f2', got %q", rows[0][1])
	}

	// Verify outcome column uses readable plain text.
	if rows[2][4] != "BLOCKED" {
		t.Fatalf("expected 'BLOCKED' in outcome column, got %q", rows[2][4])
	}

	// View should contain key elements
	view := next.View()
	if !strings.Contains(view, "Audit Trail") {
		t.Fatalf("expected 'Audit Trail' heading in view")
	}
	if !strings.Contains(view, "47 events") {
		t.Fatalf("expected '47 events' in view, got: %s", view)
	}
}

func TestAuditTUIModel_FilterActivation(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})

	// Press '/' to activate filter
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	next := updated.(auditTUIModel)
	if !next.filterActive {
		t.Fatal("expected filter to be active after '/' key")
	}

	// Press 'esc' to deactivate filter
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyEscape})
	next = updated.(auditTUIModel)
	if next.filterActive {
		t.Fatal("expected filter to be inactive after 'esc'")
	}
}

func TestAuditTUIModel_PageNavigation(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})

	// Simulate events loaded with multiple pages
	msg := auditEventsLoadedMsg{events: nil, total: 100}
	updated, _ := m.Update(msg)
	next := updated.(auditTUIModel)

	if next.totalPages != 9 { // ceil(100/12)
		t.Fatalf("expected 9 total pages, got %d", next.totalPages)
	}

	// Press 'n' for next page
	updated, cmd := next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd == nil {
		t.Fatal("expected cmd from next page")
	}
	next = updated.(auditTUIModel)
	if next.page != 1 {
		t.Fatalf("expected page 1, got %d", next.page)
	}

	// Press 'p' to go back
	updated, cmd = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if cmd == nil {
		t.Fatal("expected cmd from prev page")
	}
	next = updated.(auditTUIModel)
	if next.page != 0 {
		t.Fatalf("expected page 0, got %d", next.page)
	}

	// Press 'p' at first page — should not go negative
	updated, cmd = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if cmd != nil {
		t.Fatal("expected no cmd when at first page")
	}
	next = updated.(auditTUIModel)
	if next.page != 0 {
		t.Fatalf("expected page 0 (no change), got %d", next.page)
	}
}

func TestAuditTUIModel_Quit(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit cmd from 'q' key")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

func TestAuditTUIModel_CtrlCQuits(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd from ctrl+c")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

func TestAuditTUI_OpenClawSessionRows(t *testing.T) {
	events := []*audit.AuditEvent{
		{
			Timestamp:  time.Date(2026, 2, 28, 14, 31, 58, 0, time.UTC),
			AgentID:    "openclaw",
			SessionID:  "agent:main:discord:channel:123",
			ActionType: audit.ActionAIRequest,
			Action:     "chat",
			Outcome:    audit.OutcomeBlocked,
		},
	}

	rows := auditEventsToRows(events)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][1] != "openclaw/main:discord:123" {
		t.Fatalf("expected compact openclaw session agent label, got %q", rows[0][1])
	}
}

func TestAuditTUIModel_WindowResize(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next := updated.(auditTUIModel)

	if next.width != 120 {
		t.Fatalf("expected width 120 after resize, got %d", next.width)
	}
	if next.height != 40 {
		t.Fatalf("expected height 40, got %d", next.height)
	}
	expectedPS := 40 - 12
	if next.pageSize != expectedPS {
		t.Fatalf("expected pageSize %d after resize, got %d", expectedPS, next.pageSize)
	}
}

func TestAuditTUIModel_VerifyResult(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})

	// Press 'v' to trigger verify
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if cmd == nil {
		t.Fatal("expected cmd from 'v' key")
	}
	next := updated.(auditTUIModel)
	if !next.verifying {
		t.Fatal("expected verifying to be true")
	}

	// Simulate verify result
	updated, _ = next.Update(auditVerifyResultMsg{valid: true, total: 500})
	next = updated.(auditTUIModel)
	if next.verifying {
		t.Fatal("expected verifying to be false after result")
	}
	if !strings.Contains(next.verifyResult, "valid") {
		t.Fatalf("expected 'valid' in verify result, got: %s", next.verifyResult)
	}
	if !strings.Contains(next.verifyResult, "500") {
		t.Fatalf("expected '500' events in verify result, got: %s", next.verifyResult)
	}
}

func TestAuditTUIModel_VerifyBroken(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})
	m.verifying = true

	updated, _ := m.Update(auditVerifyResultMsg{valid: false, total: 100, brokenAt: "evt-42"})
	next := updated.(auditTUIModel)
	if !strings.Contains(next.verifyResult, "BROKEN") {
		t.Fatalf("expected 'BROKEN' in verify result, got: %s", next.verifyResult)
	}
	if !strings.Contains(next.verifyResult, "evt-42") {
		t.Fatalf("expected 'evt-42' in verify result, got: %s", next.verifyResult)
	}
}

func TestAuditTUIModel_ErrorView(t *testing.T) {
	m := newAuditTUIModel("/tmp/nonexistent.sock", map[string]interface{}{})

	msg := auditEventsLoadedMsg{err: fmt.Errorf("connection refused")}
	updated, _ := m.Update(msg)
	next := updated.(auditTUIModel)

	view := next.View()
	if !strings.Contains(view, "connection refused") {
		t.Fatalf("expected error message in view, got: %s", view)
	}
}

func TestApplyFilterToParams(t *testing.T) {
	base := map[string]interface{}{"since": "2026-01-01"}

	// outcome filter
	p := applyFilterToParams(base, "o:blocked")
	if p["outcome"] != "blocked" {
		t.Fatalf("expected outcome=blocked, got %v", p["outcome"])
	}
	if p["since"] != "2026-01-01" {
		t.Fatal("base param 'since' should be preserved")
	}

	// action filter
	p = applyFilterToParams(base, "a:tool_call")
	if p["action"] != "tool_call" {
		t.Fatalf("expected action=tool_call, got %v", p["action"])
	}

	// agent filter with prefix
	p = applyFilterToParams(base, "agent:claude")
	if p["agent"] != "claude" {
		t.Fatalf("expected agent=claude, got %v", p["agent"])
	}

	// generic filter → agent
	p = applyFilterToParams(base, "codex")
	if p["agent"] != "codex" {
		t.Fatalf("expected agent=codex for generic filter, got %v", p["agent"])
	}

	// empty filter
	p = applyFilterToParams(base, "")
	if _, ok := p["agent"]; ok {
		t.Fatal("expected no agent key for empty filter")
	}

	// base params not mutated
	if _, ok := base["outcome"]; ok {
		t.Fatal("base params should not be mutated")
	}
}

func TestAuditEventsToRows(t *testing.T) {
	events := []*audit.AuditEvent{
		{
			Timestamp:  time.Date(2026, 2, 26, 14, 32, 1, 0, time.UTC),
			AgentID:    "claude-a8f2",
			ActionType: audit.ActionAIRequest,
			Action:     "chat",
			Outcome:    audit.OutcomeSuccess,
		},
		{
			Timestamp:  time.Date(2026, 2, 26, 14, 31, 0, 0, time.UTC),
			AgentID:    "codex-1bc9",
			ActionType: audit.ActionToolCall,
			Action:     "Write",
			Outcome:    audit.OutcomeWarned,
		},
	}

	rows := auditEventsToRows(events)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Outcome should use normalized plain-text label.
	if rows[1][4] != "WARNED" {
		t.Fatalf("expected outcome 'WARNED', got %q", rows[1][4])
	}
}

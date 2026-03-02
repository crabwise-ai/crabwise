package cli

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crabwise-ai/crabwise/internal/discovery"
)

func TestAgentsTUIModel_AgentsLoaded(t *testing.T) {
	m := newAgentsTUIModel("/tmp/nonexistent.sock")

	agents := []discovery.AgentInfo{
		{ID: "claude-a8f2", Type: "claude_code", PID: 48312, Status: "active"},
		{ID: "codex-1bc9", Type: "codex_cli", PID: 48456, Status: "active"},
		{ID: "claude-f012", Type: "claude_code", PID: 0, Status: "inactive"},
	}

	msg := agentsLoadedMsg{agents: agents}
	updated, cmd := m.Update(msg)
	if cmd != nil {
		t.Fatalf("expected nil cmd from agents loaded, got %T", cmd)
	}
	next := updated.(agentsTUIModel)

	if len(next.agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(next.agents))
	}

	rows := next.table.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 table rows, got %d", len(rows))
	}

	// Verify PID column: first agent has PID, third has "—"
	if rows[0][3] != "48312" {
		t.Fatalf("expected PID '48312', got %q", rows[0][3])
	}
	if rows[2][3] != "—" {
		t.Fatalf("expected PID '—' for inactive agent, got %q", rows[2][3])
	}

	// Verify ID column
	if rows[1][1] != "codex-1bc9" {
		t.Fatalf("expected ID 'codex-1bc9', got %q", rows[1][1])
	}

	// View should contain key elements
	view := next.View()
	if !strings.Contains(view, "Agents") {
		t.Fatalf("expected 'Agents' heading in view")
	}
	if !strings.Contains(view, "2 active, 1 inactive") {
		t.Fatalf("expected summary '2 active, 1 inactive' in view, got: %s", view)
	}
}

func TestAgentsTUIModel_QuitKey(t *testing.T) {
	m := newAgentsTUIModel("/tmp/nonexistent.sock")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit cmd from 'q' key")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

func TestAgentsTUIModel_CtrlCQuits(t *testing.T) {
	m := newAgentsTUIModel("/tmp/nonexistent.sock")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd from ctrl+c")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

func TestAgentsTUIModel_WindowResize(t *testing.T) {
	m := newAgentsTUIModel("/tmp/nonexistent.sock")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next := updated.(agentsTUIModel)

	if next.width != 120 {
		t.Fatalf("expected width 120 after resize, got %d", next.width)
	}
}

func TestAgentsTUIModel_ErrorView(t *testing.T) {
	m := newAgentsTUIModel("/tmp/nonexistent.sock")

	msg := agentsLoadedMsg{err: fmt.Errorf("connection refused")}
	updated, _ := m.Update(msg)
	next := updated.(agentsTUIModel)

	view := next.View()
	if !strings.Contains(view, "connection refused") {
		t.Fatalf("expected error message in view, got: %s", view)
	}
	if !strings.Contains(view, "retry") {
		t.Fatalf("expected 'retry' hint in error view")
	}
}

func TestAgentsTUIModel_RefreshKey(t *testing.T) {
	m := newAgentsTUIModel("/tmp/nonexistent.sock")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("expected load cmd from 'r' key")
	}
}

func TestAgentsSummary(t *testing.T) {
	agents := []discovery.AgentInfo{
		{Status: "active"},
		{Status: "active"},
		{Status: "inactive"},
	}
	got := agentsSummary(agents)
	if got != "2 active, 1 inactive" {
		t.Fatalf("expected '2 active, 1 inactive', got %q", got)
	}
}

func TestAgentsSummary_Empty(t *testing.T) {
	got := agentsSummary(nil)
	if got != "0 active, 0 inactive" {
		t.Fatalf("expected '0 active, 0 inactive', got %q", got)
	}
}

func TestAgentsTUIModel_OpenClawSessionsHaveNoPID(t *testing.T) {
	m := newAgentsTUIModel("/tmp/nonexistent.sock")

	agents := []discovery.AgentInfo{
		{ID: "openclaw/agent:main:discord:channel:123", Type: "openclaw", PID: 0, Status: "active"},
	}

	updated, _ := m.Update(agentsLoadedMsg{agents: agents})
	next := updated.(agentsTUIModel)

	rows := next.table.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 table row, got %d", len(rows))
	}
	if rows[0][1] != "openclaw/agent:main:discord:channel:123" {
		t.Fatalf("expected full openclaw id in row, got %q", rows[0][1])
	}
	if rows[0][3] != "—" {
		t.Fatalf("expected PID dash for openclaw session, got %q", rows[0][3])
	}
}

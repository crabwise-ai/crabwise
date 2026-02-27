package cli

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crabwise-ai/crabwise/internal/daemon"
)

func TestCommandmentsTUIModel_LoadedMsg(t *testing.T) {
	m := newCommandmentsTUIModel("/tmp/nonexistent.sock")

	rules := []daemon.CommandmentRuleSummary{
		{Name: "no-destructive-commands", Enforcement: "block", Priority: 100, Enabled: true},
		{Name: "no-credential-access", Enforcement: "warn", Priority: 90, Enabled: true},
		{Name: "approved-models-only", Enforcement: "block", Priority: 80, Enabled: true},
		{Name: "no-git-push-main", Enforcement: "warn", Priority: 70, Enabled: false},
	}

	updated, cmd := m.Update(commandmentsLoadedMsg{rules: rules})
	if cmd != nil {
		t.Fatalf("expected nil cmd from loaded msg, got %T", cmd)
	}
	next := updated.(commandmentsTUIModel)

	if next.ruleCount != 4 {
		t.Fatalf("expected 4 rules, got %d", next.ruleCount)
	}
	if len(next.rules) != 4 {
		t.Fatalf("expected 4 rules stored, got %d", len(next.rules))
	}

	// Verify table rows
	rows := next.table.Rows()
	if len(rows) != 4 {
		t.Fatalf("expected 4 table rows, got %d", len(rows))
	}
	if rows[0][0] != "no-destructive-commands" {
		t.Fatalf("expected first row name 'no-destructive-commands', got %q", rows[0][0])
	}
	if rows[0][1] != "block" {
		t.Fatalf("expected first row enforcement 'block', got %q", rows[0][1])
	}
	if rows[0][2] != "100" {
		t.Fatalf("expected first row priority '100', got %q", rows[0][2])
	}
	if rows[0][3] != "✓" {
		t.Fatalf("expected first row enabled '✓', got %q", rows[0][3])
	}
	if rows[3][3] != "○" {
		t.Fatalf("expected fourth row disabled '○', got %q", rows[3][3])
	}

	// View should contain key elements
	view := next.View()
	if !strings.Contains(view, "Commandments") {
		t.Fatalf("expected 'Commandments' heading in view, got: %s", view)
	}
	if !strings.Contains(view, "4 rules loaded") {
		t.Fatalf("expected '4 rules loaded' in view, got: %s", view)
	}
	if !strings.Contains(view, "navigate") {
		t.Fatalf("expected navigation hint in view, got: %s", view)
	}
}

func TestCommandmentsTUIModel_QuitKey(t *testing.T) {
	m := newCommandmentsTUIModel("/tmp/nonexistent.sock")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit cmd from 'q' key")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

func TestCommandmentsTUIModel_CtrlCQuits(t *testing.T) {
	m := newCommandmentsTUIModel("/tmp/nonexistent.sock")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd from ctrl+c")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

func TestCommandmentsTUIModel_ReloadKey(t *testing.T) {
	m := newCommandmentsTUIModel("/tmp/nonexistent.sock")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("expected reload cmd from 'r' key")
	}
	next := updated.(commandmentsTUIModel)
	if !next.reloading {
		t.Fatal("expected reloading to be true after 'r' key")
	}
}

func TestCommandmentsTUIModel_WindowResize(t *testing.T) {
	m := newCommandmentsTUIModel("/tmp/nonexistent.sock")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next := updated.(commandmentsTUIModel)

	if next.width != 120 {
		t.Fatalf("expected width 120 after resize, got %d", next.width)
	}
}

func TestCommandmentsTUIModel_EmptyRules(t *testing.T) {
	m := newCommandmentsTUIModel("/tmp/nonexistent.sock")

	updated, _ := m.Update(commandmentsLoadedMsg{rules: []daemon.CommandmentRuleSummary{}})
	next := updated.(commandmentsTUIModel)

	if next.ruleCount != 0 {
		t.Fatalf("expected 0 rules, got %d", next.ruleCount)
	}

	view := next.View()
	if !strings.Contains(view, "No commandments loaded") {
		t.Fatalf("expected 'No commandments loaded' in view, got: %s", view)
	}
}

func TestCommandmentsTUIModel_LoadError(t *testing.T) {
	m := newCommandmentsTUIModel("/tmp/nonexistent.sock")

	updated, _ := m.Update(commandmentsLoadedMsg{err: errTestDial})
	next := updated.(commandmentsTUIModel)

	if next.err == nil {
		t.Fatal("expected error to be set")
	}

	view := next.View()
	if !strings.Contains(view, "retry") {
		t.Fatalf("expected 'retry' hint in error view, got: %s", view)
	}
}

func TestCommandmentsTUIModel_ReloadedMsg(t *testing.T) {
	m := newCommandmentsTUIModel("/tmp/nonexistent.sock")
	m.reloading = true

	updated, cmd := m.Update(commandmentsReloadedMsg{rulesLoaded: 5})
	next := updated.(commandmentsTUIModel)

	if next.reloading {
		t.Fatal("expected reloading to be false after reload msg")
	}
	if !strings.Contains(next.reloadMsg, "5 rules") {
		t.Fatalf("expected reload msg to mention 5 rules, got %q", next.reloadMsg)
	}
	if cmd == nil {
		t.Fatal("expected re-list cmd after reload")
	}
}

var errTestDial = fmt.Errorf("test dial error")

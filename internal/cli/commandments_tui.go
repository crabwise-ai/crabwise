package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/tui"
)

// commandmentsLoadedMsg carries the result of listing commandments via IPC.
type commandmentsLoadedMsg struct {
	rules []daemon.CommandmentRuleSummary
	err   error
}

// commandmentsReloadedMsg carries the result of reloading commandments via IPC.
type commandmentsReloadedMsg struct {
	rulesLoaded int
	err         error
}

type commandmentsTUIModel struct {
	socketPath string
	table      table.Model
	rules      []daemon.CommandmentRuleSummary
	width      int
	height     int
	ruleCount  int
	reloading  bool
	reloadMsg  string
	err        error
}

func newCommandmentsTUIModel(socketPath string) commandmentsTUIModel {
	cols := commandmentsColumns()
	t := tui.NewStyledTable(cols, nil, tui.WithHeight(10))
	t.Focus()

	return commandmentsTUIModel{
		socketPath: socketPath,
		table:      t,
		width:      80,
		height:     24,
	}
}

func commandmentsColumns() []table.Column {
	return []table.Column{
		{Title: "NAME", Width: 33},
		{Title: "ENFORCEMENT", Width: 13},
		{Title: "PRI", Width: 5},
		{Title: "ENABLED", Width: 9},
	}
}

func (m commandmentsTUIModel) Init() tea.Cmd {
	return loadCommandments(m.socketPath)
}

func (m commandmentsTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.reloading = true
			m.reloadMsg = ""
			return m, reloadCommandments(m.socketPath)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		h := msg.Height - 10
		if h < 3 {
			h = 3
		}
		m.table.SetHeight(h)

	case commandmentsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.rules = msg.rules
		m.ruleCount = len(msg.rules)
		m.table.SetRows(commandmentsRows(msg.rules))
		h := m.height - 10
		if h < 3 {
			h = 3
		}
		m.table.SetHeight(h)

	case commandmentsReloadedMsg:
		m.reloading = false
		if msg.err != nil {
			m.reloadMsg = fmt.Sprintf("✖ Reload failed: %v", msg.err)
		} else {
			m.reloadMsg = fmt.Sprintf("✓ Reloaded %d rules", msg.rulesLoaded)
		}
		return m, loadCommandments(m.socketPath)
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m commandmentsTUIModel) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	var b strings.Builder

	// Banner
	b.WriteString(renderCommandmentsBanner(m.ruleCount))
	b.WriteString("\n\n")

	if m.err != nil {
		fmt.Fprintf(&b, "  %s %s\n", tui.StatusIcon("error"), tui.StyleError.Render(m.err.Error()))
		b.WriteString("\n")
		b.WriteString(tui.RenderStatusBar("r retry  q quit", "", w))
		return b.String()
	}

	if m.ruleCount == 0 {
		fmt.Fprintf(&b, "  %s\n", tui.StyleMuted.Render("No commandments loaded."))
		b.WriteString("\n")
		b.WriteString(tui.RenderStatusBar("r reload  q quit", "", w))
		return b.String()
	}

	// Table
	b.WriteString("  ")
	b.WriteString(m.table.View())
	b.WriteString("\n")

	// Reload message
	if m.reloadMsg != "" {
		if strings.HasPrefix(m.reloadMsg, "✓") {
			fmt.Fprintf(&b, "  %s\n", tui.StyleSuccess.Render(m.reloadMsg))
		} else {
			fmt.Fprintf(&b, "  %s\n", tui.StyleError.Render(m.reloadMsg))
		}
	}

	b.WriteString("\n")

	// Status bar
	hint := "↑↓ navigate  r reload  q quit"
	if m.reloading {
		hint = "reloading..."
	}
	b.WriteString(tui.RenderStatusBar(hint, "", w))

	return b.String()
}

func renderCommandmentsBanner(ruleCount int) string {
	art := tui.CrabArt
	gap := "  "
	rightText := []string{
		tui.StyleHeading.Render("Commandments"),
		"",
		tui.StyleMuted.Render(fmt.Sprintf("%d rules loaded", ruleCount)),
	}
	var lines []string
	for i, a := range art {
		styled := lipgloss.NewStyle().Foreground(tui.ColorCrabOrange).Render(a)
		right := ""
		if i < len(rightText) {
			right = rightText[i]
		}
		lines = append(lines, styled+gap+right)
	}
	return strings.Join(lines, "\n")
}

func commandmentsRows(rules []daemon.CommandmentRuleSummary) []table.Row {
	rows := make([]table.Row, 0, len(rules))
	for _, rule := range rules {
		var enabled string
		if rule.Enabled {
			enabled = "✓"
		} else {
			enabled = "○"
		}
		rows = append(rows, table.Row{
			rule.Name,
			rule.Enforcement,
			fmt.Sprintf("%d", rule.Priority),
			enabled,
		})
	}
	return rows
}

func loadCommandments(socketPath string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return commandmentsLoadedMsg{err: fmt.Errorf("connect to daemon: %w", err)}
		}
		defer client.Close()

		result, err := client.Call("commandments.list", nil)
		if err != nil {
			return commandmentsLoadedMsg{err: fmt.Errorf("commandments.list: %w", err)}
		}

		var rules []daemon.CommandmentRuleSummary
		if err := json.Unmarshal(result, &rules); err != nil {
			return commandmentsLoadedMsg{err: fmt.Errorf("parse result: %w", err)}
		}

		return commandmentsLoadedMsg{rules: rules}
	}
}

func reloadCommandments(socketPath string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return commandmentsReloadedMsg{err: fmt.Errorf("connect to daemon: %w", err)}
		}
		defer client.Close()

		result, err := client.Call("commandments.reload", nil)
		if err != nil {
			return commandmentsReloadedMsg{err: fmt.Errorf("commandments.reload: %w", err)}
		}

		var out struct {
			OK          bool `json:"ok"`
			RulesLoaded int  `json:"rules_loaded"`
		}
		if err := json.Unmarshal(result, &out); err != nil {
			return commandmentsReloadedMsg{err: fmt.Errorf("parse result: %w", err)}
		}

		return commandmentsReloadedMsg{rulesLoaded: out.RulesLoaded}
	}
}

func runCommandmentsTUI(socketPath string) error {
	m := newCommandmentsTUIModel(socketPath)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

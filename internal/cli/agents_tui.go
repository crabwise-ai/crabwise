package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crabwise-ai/crabwise/internal/discovery"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/tui"
)

// agentsLoadedMsg carries the result of an IPC agents.list call.
type agentsLoadedMsg struct {
	agents []discovery.AgentInfo
	err    error
}

type agentsTUIModel struct {
	socketPath string
	table      table.Model
	agents     []discovery.AgentInfo
	width      int
	err        error
}

func newAgentsTUIModel(socketPath string) agentsTUIModel {
	cols := agentsColumns(80)
	t := tui.NewStyledTable(cols, nil, tui.WithHeight(10))
	t.Focus()

	return agentsTUIModel{
		socketPath: socketPath,
		table:      t,
		width:      80,
	}
}

func agentsColumns(width int) []table.Column {
	return []table.Column{
		{Title: "STATUS", Width: 8},
		{Title: "ID", Width: 30},
		{Title: "TYPE", Width: 12},
		{Title: "PID", Width: 10},
	}
}

func (m agentsTUIModel) Init() tea.Cmd {
	return loadAgents(m.socketPath)
}

func (m agentsTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, loadAgents(m.socketPath)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.table.SetWidth(msg.Width)

	case agentsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.agents = nil
			m.table.SetRows(nil)
			return m, nil
		}
		m.err = nil
		m.agents = msg.agents
		m.table.SetRows(agentsToRows(msg.agents))
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m agentsTUIModel) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	var b strings.Builder

	// Banner
	b.WriteString(renderAgentsBanner())
	b.WriteString("\n\n")

	if m.err != nil {
		fmt.Fprintf(&b, "  %s %s\n",
			tui.StatusIcon("error"),
			tui.StyleError.Render(m.err.Error()),
		)
		b.WriteString("\n")
		b.WriteString(tui.RenderStatusBar("r retry  q quit", "", w))
		return b.String()
	}

	// Table
	b.WriteString(m.table.View())
	b.WriteString("\n\n")

	// Summary
	b.WriteString("  ")
	b.WriteString(tui.StyleMuted.Render(agentsSummary(m.agents)))
	b.WriteString("\n\n")

	// Status bar
	b.WriteString(tui.RenderStatusBar("↑↓ navigate  r refresh  q quit", "", w))

	return b.String()
}

func renderAgentsBanner() string {
	gap := "  "
	rightText := []string{
		tui.StyleHeading.Render("Agents"),
	}
	var lines []string
	for i, a := range tui.CrabArt {
		styled := lipgloss.NewStyle().Foreground(tui.ColorCrabOrange).Render(a)
		right := ""
		if i < len(rightText) {
			right = rightText[i]
		}
		lines = append(lines, styled+gap+right)
	}
	return strings.Join(lines, "\n")
}

func agentsToRows(agents []discovery.AgentInfo) []table.Row {
	rows := make([]table.Row, len(agents))
	for i, a := range agents {
		pid := "—"
		if a.PID != 0 {
			pid = fmt.Sprintf("%d", a.PID)
		}
		rows[i] = table.Row{
			tui.StatusIcon(a.Status),
			a.ID,
			a.Type,
			pid,
		}
	}
	return rows
}

func agentsSummary(agents []discovery.AgentInfo) string {
	active := 0
	for _, a := range agents {
		if strings.EqualFold(a.Status, "active") || strings.EqualFold(a.Status, "running") {
			active++
		}
	}
	inactive := len(agents) - active
	return fmt.Sprintf("%d active, %d inactive", active, inactive)
}

func loadAgents(socketPath string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return agentsLoadedMsg{err: fmt.Errorf("connect to daemon: %w", err)}
		}
		defer client.Close()

		result, err := client.Call("agents.list", nil)
		if err != nil {
			return agentsLoadedMsg{err: fmt.Errorf("agents.list: %w", err)}
		}

		var agents []discovery.AgentInfo
		if err := json.Unmarshal(result, &agents); err != nil {
			return agentsLoadedMsg{err: fmt.Errorf("parse agents: %w", err)}
		}

		return agentsLoadedMsg{agents: agents}
	}
}

func runAgentsTUI(socketPath string) error {
	m := newAgentsTUIModel(socketPath)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

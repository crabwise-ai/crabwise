package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/tui"
)

// Messages for async IPC operations.
type auditEventsLoadedMsg struct {
	events []*audit.AuditEvent
	total  int
	err    error
}

type auditCostLoadedMsg struct {
	rows      []audit.CostSummaryRow
	totalCost float64
	err       error
}

type auditVerifyResultMsg struct {
	valid    bool
	total    int
	brokenAt string
	err      error
}

type auditTUIModel struct {
	socketPath string
	width      int
	height     int
	mode       string // "events" or "cost"

	// Event mode fields
	table        table.Model
	events       []*audit.AuditEvent
	total        int
	page         int
	pageSize     int
	totalPages   int
	filterActive bool
	filterInput  textinput.Model
	filterText   string // applied filter

	// Cost mode fields
	costTable table.Model
	costRows  []audit.CostSummaryRow
	totalCost float64

	// Verification fields
	verifying    bool
	verifyResult string

	// Base query params from CLI flags
	queryParams map[string]interface{}

	err error
}

func newAuditTUIModel(socketPath string, queryParams map[string]interface{}, initialMode string) auditTUIModel {
	cols := auditEventsColumns()
	t := tui.NewStyledTable(cols, nil, tui.WithHeight(10))
	t.Focus()

	costCols := auditCostColumns()
	ct := tui.NewStyledTable(costCols, nil, tui.WithHeight(10))

	ti := textinput.New()
	ti.Placeholder = "filter (o:blocked  a:tool_call  agent:name)"
	ti.CharLimit = 64
	ti.Width = 50

	mode := "events"
	if initialMode == "cost" {
		mode = "cost"
	}

	return auditTUIModel{
		socketPath:  socketPath,
		width:       80,
		height:      24,
		mode:        mode,
		table:       t,
		costTable:   ct,
		filterInput: ti,
		queryParams: queryParams,
		pageSize:    12,
	}
}

func auditEventsColumns() []table.Column {
	return []table.Column{
		{Title: "TIME", Width: 8},
		{Title: "AGENT", Width: 14},
		{Title: "ACTION TYPE", Width: 19},
		{Title: "ACTION", Width: 11},
		{Title: "OUTCOME", Width: 11},
		{Title: "COST", Width: 7},
	}
}

func auditCostColumns() []table.Column {
	return []table.Column{
		{Title: "DAY", Width: 12},
		{Title: "AGENT", Width: 14},
		{Title: "MODEL", Width: 24},
		{Title: "IN", Width: 8},
		{Title: "OUT", Width: 8},
		{Title: "COST", Width: 8},
	}
}

func (m auditTUIModel) Init() tea.Cmd {
	if m.mode == "cost" {
		return loadAuditCost(m.socketPath, m.queryParams)
	}
	return loadAuditEvents(m.socketPath, m.queryParams, m.page, m.pageSize, "")
}

func (m auditTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.filterActive {
			switch msg.String() {
			case "enter":
				m.filterText = m.filterInput.Value()
				m.filterActive = false
				m.filterInput.Blur()
				m.page = 0
				return m, loadAuditEvents(m.socketPath, m.queryParams, 0, m.pageSize, m.filterText)
			case "esc":
				m.filterActive = false
				m.filterInput.Blur()
				if m.filterText != "" {
					m.filterText = ""
					m.filterInput.SetValue("")
					m.page = 0
					return m, loadAuditEvents(m.socketPath, m.queryParams, 0, m.pageSize, "")
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "/":
			if m.mode == "events" {
				m.filterActive = true
				m.filterInput.Focus()
				return m, m.filterInput.Cursor.BlinkCmd()
			}
		case "c":
			if m.mode == "events" {
				m.mode = "cost"
				return m, loadAuditCost(m.socketPath, m.queryParams)
			}
			m.mode = "events"
			return m, loadAuditEvents(m.socketPath, m.queryParams, m.page, m.pageSize, m.filterText)
		case "v":
			if m.mode == "events" && !m.verifying {
				m.verifying = true
				m.verifyResult = ""
				return m, verifyAuditIntegrity(m.socketPath)
			}
		case "n", "right":
			if m.mode == "events" && m.page < m.totalPages-1 {
				m.page++
				return m, loadAuditEvents(m.socketPath, m.queryParams, m.page, m.pageSize, m.filterText)
			}
		case "p", "left":
			if m.mode == "events" && m.page > 0 {
				m.page--
				return m, loadAuditEvents(m.socketPath, m.queryParams, m.page, m.pageSize, m.filterText)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		ps := msg.Height - 12
		if ps < 5 {
			ps = 5
		}
		m.pageSize = ps
		m.table.SetWidth(msg.Width)
		m.table.SetHeight(ps)
		m.costTable.SetWidth(msg.Width)
		m.costTable.SetHeight(ps)

	case auditEventsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.events = msg.events
		m.total = msg.total
		m.totalPages = (msg.total + m.pageSize - 1) / m.pageSize
		if m.totalPages < 1 {
			m.totalPages = 1
		}
		m.table.SetRows(auditEventsToRows(msg.events))
		return m, nil

	case auditCostLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.costRows = msg.rows
		m.totalCost = msg.totalCost
		m.costTable.SetRows(auditCostToRows(msg.rows))
		return m, nil

	case auditVerifyResultMsg:
		m.verifying = false
		switch {
		case msg.err != nil:
			m.verifyResult = tui.StatusIcon("error") + " " + tui.StyleError.Render(msg.err.Error())
		case msg.valid:
			m.verifyResult = tui.StatusIcon("success") + " " + tui.StyleSuccess.Render(fmt.Sprintf("Hash chain valid (%d events)", msg.total))
		default:
			m.verifyResult = tui.StatusIcon("blocked") + " " + tui.StyleError.Render(fmt.Sprintf("Hash chain BROKEN at event %s (%d events checked)", msg.brokenAt, msg.total))
		}
		return m, nil
	}

	// Forward to active table for ↑↓ navigation
	if m.mode == "events" && !m.filterActive {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	if m.mode == "cost" {
		var cmd tea.Cmd
		m.costTable, cmd = m.costTable.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m auditTUIModel) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	var b strings.Builder

	if m.mode == "cost" {
		b.WriteString(renderAuditBanner("Audit — Cost Summary", ""))
	} else {
		countInfo := fmt.Sprintf("%d events", m.total)
		b.WriteString(renderAuditBanner("Audit Trail", countInfo))
	}
	b.WriteString("\n")

	// Filter status line (events mode only)
	if m.mode == "events" {
		if m.filterActive {
			b.WriteString("  Filter: " + m.filterInput.View())
		} else if m.filterText != "" {
			b.WriteString("  " + tui.StyleMuted.Render("Filter: ") + tui.StyleBody.Render(m.filterText))
		}
		pageInfo := fmt.Sprintf("Page %d/%d", m.page+1, m.totalPages)
		b.WriteString("  " + tui.StyleMuted.Render(pageInfo))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Error display
	if m.err != nil {
		fmt.Fprintf(&b, "  %s %s\n",
			tui.StatusIcon("error"),
			tui.StyleError.Render(m.err.Error()),
		)
		b.WriteString("\n")
		b.WriteString(tui.RenderStatusBar("r retry  q quit", "", w))
		return b.String()
	}

	if m.mode == "cost" {
		b.WriteString(m.costTable.View())
		b.WriteString("\n\n")
		b.WriteString("  " + tui.StyleBody.Render("Total: ") + tui.StyleHeading.Render(tui.FormatCost(m.totalCost)))
		b.WriteString("\n\n")
		b.WriteString(tui.RenderStatusBar("↑↓ navigate  c events view  q quit", "", w))
	} else {
		b.WriteString(m.table.View())
		b.WriteString("\n")

		// Verification result
		if m.verifying {
			b.WriteString("  " + tui.StyleMuted.Render("Verifying integrity..."))
			b.WriteString("\n")
		} else if m.verifyResult != "" {
			b.WriteString("  " + m.verifyResult)
			b.WriteString("\n")
		}

		b.WriteString("\n")

		pageInfo := fmt.Sprintf("Page %d/%d", m.page+1, m.totalPages)
		b.WriteString(tui.RenderStatusBar("↑↓ navigate  / filter  c cost view  v verify  n/p page  q quit", pageInfo, w))
	}

	return b.String()
}

func renderAuditBanner(heading, rightInfo string) string {
	gap := "  "
	rightText := make([]string, len(tui.CrabArt))
	rightText[0] = tui.StyleHeading.Render(heading)
	if rightInfo != "" {
		rightText[0] += "  " + tui.StyleMuted.Render(rightInfo)
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

func auditEventsToRows(events []*audit.AuditEvent) []table.Row {
	rows := make([]table.Row, len(events))
	for i, e := range events {
		ts := tui.FormatTimestamp(e.Timestamp)
		agent := tui.Truncate(e.AgentID, 14)
		actionType := string(e.ActionType)
		action := tui.Truncate(e.Action, 11)
		outcome := tui.StatusIcon(string(e.Outcome)) + " " + string(e.Outcome)
		cost := ""
		if e.ActionType == audit.ActionAIRequest && e.CostUSD > 0 {
			cost = tui.FormatCost(e.CostUSD)
		}
		rows[i] = table.Row{ts, agent, actionType, action, outcome, cost}
	}
	return rows
}

func auditCostToRows(costRows []audit.CostSummaryRow) []table.Row {
	rows := make([]table.Row, len(costRows))
	for i, r := range costRows {
		rows[i] = table.Row{
			r.Day,
			tui.Truncate(r.AgentID, 14),
			tui.Truncate(r.Model, 24),
			fmt.Sprintf("%d", r.InputTokens),
			fmt.Sprintf("%d", r.OutputTokens),
			tui.FormatCost(r.CostUSD),
		}
	}
	return rows
}

// applyFilterToParams merges the filter text into query params using prefix conventions.
func applyFilterToParams(base map[string]interface{}, filter string) map[string]interface{} {
	params := make(map[string]interface{}, len(base))
	for k, v := range base {
		params[k] = v
	}
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return params
	}

	switch {
	case strings.HasPrefix(filter, "o:"):
		params["outcome"] = strings.TrimPrefix(filter, "o:")
	case strings.HasPrefix(filter, "a:"):
		params["action"] = strings.TrimPrefix(filter, "a:")
	case strings.HasPrefix(filter, "agent:"):
		params["agent"] = strings.TrimPrefix(filter, "agent:")
	default:
		params["agent"] = filter
	}
	return params
}

func loadAuditEvents(socketPath string, baseParams map[string]interface{}, page, pageSize int, filter string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return auditEventsLoadedMsg{err: fmt.Errorf("connect to daemon: %w", err)}
		}
		defer client.Close()

		params := applyFilterToParams(baseParams, filter)
		params["limit"] = pageSize
		params["offset"] = page * pageSize

		result, err := client.Call("audit.query", params)
		if err != nil {
			return auditEventsLoadedMsg{err: fmt.Errorf("audit.query: %w", err)}
		}

		var qr audit.QueryResult
		if err := json.Unmarshal(result, &qr); err != nil {
			return auditEventsLoadedMsg{err: fmt.Errorf("parse result: %w", err)}
		}

		return auditEventsLoadedMsg{events: qr.Events, total: qr.Total}
	}
}

func loadAuditCost(socketPath string, baseParams map[string]interface{}) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return auditCostLoadedMsg{err: fmt.Errorf("connect to daemon: %w", err)}
		}
		defer client.Close()

		allowed := map[string]interface{}{}
		if v, ok := baseParams["since"]; ok {
			allowed["since"] = v
		}
		if v, ok := baseParams["until"]; ok {
			allowed["until"] = v
		}
		if v, ok := baseParams["agent"]; ok {
			allowed["agent"] = v
		}

		result, err := client.Call("audit.cost", allowed)
		if err != nil {
			return auditCostLoadedMsg{err: fmt.Errorf("audit.cost: %w", err)}
		}

		var rows []audit.CostSummaryRow
		if err := json.Unmarshal(result, &rows); err != nil {
			return auditCostLoadedMsg{err: fmt.Errorf("parse cost summary: %w", err)}
		}

		var total float64
		for _, r := range rows {
			total += r.CostUSD
		}

		return auditCostLoadedMsg{rows: rows, totalCost: total}
	}
}

func verifyAuditIntegrity(socketPath string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return auditVerifyResultMsg{err: fmt.Errorf("connect to daemon: %w", err)}
		}
		defer client.Close()

		result, err := client.Call("audit.verify", nil)
		if err != nil {
			return auditVerifyResultMsg{err: fmt.Errorf("audit.verify: %w", err)}
		}

		var v struct {
			Valid    bool   `json:"valid"`
			Total    int    `json:"total"`
			BrokenAt string `json:"broken_at"`
		}
		if err := json.Unmarshal(result, &v); err != nil {
			return auditVerifyResultMsg{err: fmt.Errorf("parse verify result: %w", err)}
		}

		return auditVerifyResultMsg{valid: v.Valid, total: v.Total, brokenAt: v.BrokenAt}
	}
}

func runAuditTUI(socketPath string, queryParams map[string]interface{}, initialMode string) error {
	m := newAuditTUIModel(socketPath, queryParams, initialMode)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

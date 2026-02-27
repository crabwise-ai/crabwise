package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/tui"
)

type watchConn struct {
	client  *ipc.Client
	scanner *bufio.Scanner
}

type watchModelDeps struct {
	Now            func() time.Time
	ReconnectDelay time.Duration
	StatusInterval time.Duration
	Reconnect      func() tea.Msg
	PollStatus     func() tea.Msg
}

type watchModel struct {
	feed               []string
	allEvents          []feedEntry
	queueDepth         int
	queueDropped       uint64
	daemonUptime       string
	startedAt          time.Time
	triggerTimes       []time.Time
	triggersLastMinute int
	reconnectAttempts  int
	fatalErr           error

	connected  bool
	width      int
	bannerTick int

	filterMode  bool
	filterInput textinput.Model
	filterText  string

	client  *ipc.Client
	scanner *bufio.Scanner
	deps    watchModelDeps
}

type feedEntry struct {
	formatted  string
	outcome    audit.Outcome
	searchable string
}

type watchStreamLineMsg struct {
	line []byte
}

type streamDisconnectedMsg struct {
	Err error
}

type reconnectMsg struct{}

type reconnectResultMsg struct {
	Conn watchConn
	Err  error
}

type statusTickMsg struct{}

type watchBannerTickMsg struct{}

type statusResultMsg struct {
	OK           bool
	QueueDepth   int
	QueueDropped uint64
	Uptime       string
}

func pollDaemonStatus(socketPath string) tea.Msg {
	client, err := ipc.Dial(socketPath)
	if err != nil {
		return statusResultMsg{OK: false}
	}
	defer client.Close()

	result, err := client.Call("status", nil)
	if err != nil {
		return statusResultMsg{OK: false}
	}

	var s struct {
		QueueDepth   int    `json:"queue_depth"`
		QueueDropped uint64 `json:"queue_dropped"`
		Uptime       string `json:"uptime"`
	}
	if err := json.Unmarshal(result, &s); err != nil {
		return statusResultMsg{OK: false}
	}

	return statusResultMsg{
		OK:           true,
		QueueDepth:   s.QueueDepth,
		QueueDropped: s.QueueDropped,
		Uptime:       s.Uptime,
	}
}

func runWatchTUI(cfg *daemon.Config) error {
	conn, err := openWatchStream(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}

	m := newWatchModel(watchModelDeps{
		Now:            time.Now,
		ReconnectDelay: 1500 * time.Millisecond,
		StatusInterval: 3 * time.Second,
		Reconnect: func() tea.Msg {
			reconnected, reconnectErr := openWatchStream(cfg.Daemon.SocketPath)
			if reconnectErr != nil {
				return reconnectResultMsg{Err: reconnectErr}
			}
			return reconnectResultMsg{Conn: reconnected}
		},
		PollStatus: func() tea.Msg {
			return pollDaemonStatus(cfg.Daemon.SocketPath)
		},
	})
	m.client = conn.client
	m.scanner = conn.scanner
	m.connected = true

	program := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := program.Run()
	if err != nil {
		return err
	}

	final := finalModel.(watchModel)
	if final.client != nil {
		_ = final.client.Close()
	}
	if final.fatalErr != nil {
		return final.fatalErr
	}

	return nil
}

func openWatchStream(socketPath string) (watchConn, error) {
	client, err := ipc.Dial(socketPath)
	if err != nil {
		return watchConn{}, fmt.Errorf("connect to daemon: %w", err)
	}

	scanner, err := client.Subscribe("audit.subscribe", nil)
	if err != nil {
		_ = client.Close()
		return watchConn{}, fmt.Errorf("subscribe: %w", err)
	}

	return watchConn{client: client, scanner: scanner}, nil
}

func newWatchModel(deps watchModelDeps) watchModel {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.ReconnectDelay <= 0 {
		deps.ReconnectDelay = 1500 * time.Millisecond
	}
	if deps.StatusInterval <= 0 {
		deps.StatusInterval = 3 * time.Second
	}

	now := deps.Now()
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 64
	ti.Width = 40

	return watchModel{
		feed:        make([]string, 0, 16),
		allEvents:   make([]feedEntry, 0, 16),
		startedAt:   now,
		width:       80,
		filterInput: ti,
		deps:        deps,
	}
}

func (m watchModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.scanner != nil {
		cmds = append(cmds, readWatchStreamCmd(m.scanner))
	}
	cmds = append(cmds, tea.Tick(m.deps.StatusInterval, func(time.Time) tea.Msg {
		return statusTickMsg{}
	}))
	cmds = append(cmds, tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg {
		return watchBannerTickMsg{}
	}))
	return tea.Batch(cmds...)
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.width < 40 {
			m.width = 40
		}
		return m, nil

	case tea.KeyMsg:
		if m.filterMode {
			switch msg.String() {
			case "enter":
				m.filterText = m.filterInput.Value()
				m.filterMode = false
				m.filterInput.Blur()
				m.rebuildFeed()
				return m, nil
			case "esc":
				m.filterText = ""
				m.filterMode = false
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.rebuildFeed()
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
			m.filterMode = true
			m.filterInput.Focus()
			return m, m.filterInput.Cursor.BlinkCmd()
		}

	case watchStreamLineMsg:
		m.handleStreamLine(msg.line)
		if m.fatalErr != nil {
			return m, tea.Quit
		}
		if m.scanner == nil {
			return m, nil
		}
		return m, readWatchStreamCmd(m.scanner)

	case auditEventMsg:
		m.recordAuditEvent(msg.Event)
		return m, nil

	case statusTickMsg:
		var cmds []tea.Cmd
		if m.deps.PollStatus != nil {
			cmds = append(cmds, func() tea.Msg { return m.deps.PollStatus() })
		}
		cmds = append(cmds, tea.Tick(m.deps.StatusInterval, func(time.Time) tea.Msg {
			return statusTickMsg{}
		}))
		return m, tea.Batch(cmds...)

	case statusResultMsg:
		if msg.OK {
			m.queueDepth = msg.QueueDepth
			m.queueDropped = msg.QueueDropped
			if msg.Uptime != "" {
				m.daemonUptime = msg.Uptime
			}
		}
		return m, nil

	case streamDisconnectedMsg:
		if m.client != nil {
			_ = m.client.Close()
			m.client = nil
		}
		m.scanner = nil
		m.connected = false
		m.appendFeed(fmt.Sprintf("stream disconnected: %v", msg.Err))

		if m.reconnectAttempts >= 1 {
			m.fatalErr = fmt.Errorf("watch stream disconnected after one reconnect attempt; rerun with `crabwise watch` or use `crabwise watch --text`: %w", msg.Err)
			return m, tea.Quit
		}

		m.reconnectAttempts++
		m.appendFeed("attempting reconnect in 1.5s")
		return m, tea.Tick(m.deps.ReconnectDelay, func(time.Time) tea.Msg {
			return reconnectMsg{}
		})

	case reconnectMsg:
		if m.deps.Reconnect == nil {
			m.fatalErr = errors.New("watch stream disconnected and reconnect is unavailable; rerun with `crabwise watch --text`")
			return m, tea.Quit
		}
		return m, func() tea.Msg { return m.deps.Reconnect() }

	case reconnectResultMsg:
		if msg.Err != nil {
			m.fatalErr = fmt.Errorf("watch stream reconnect failed; check daemon status and retry, or use `crabwise watch --text`: %w", msg.Err)
			return m, tea.Quit
		}
		m.client = msg.Conn.client
		m.scanner = msg.Conn.scanner
		m.connected = true
		m.appendFeed("reconnected")
		if m.scanner == nil {
			m.fatalErr = errors.New("watch stream reconnect returned no scanner")
			return m, tea.Quit
		}
		return m, readWatchStreamCmd(m.scanner)

	case watchBannerTickMsg:
		m.bannerTick++
		return m, tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg {
			return watchBannerTickMsg{}
		})
	}

	return m, nil
}

func (m watchModel) View() string {
	var lines []string

	// Banner area: crab art on left (ripple animation), Watch title + connection on right
	art := tui.CrabArtRipple(m.bannerTick)
	connIndicator := m.connectionIndicator()
	bannerRight := []string{
		tui.StyleHeading.Render("Watch") + strings.Repeat(" ", max(1, m.width-lipgloss.Width(tui.StyleHeading.Render("Watch"))-len(art[0])-3-lipgloss.Width(connIndicator))) + connIndicator,
		tui.StyleDivider(27),
	}
	// Status strip values
	uptime := m.daemonUptime
	if uptime == "" {
		uptime = tui.FormatDuration(m.deps.Now().Sub(m.startedAt))
	}
	statusLine := tui.StyleMuted.Render("Queue: ") + tui.StyleBody.Render(fmt.Sprintf("%d", m.queueDepth)) +
		tui.StyleMuted.Render("  Dropped: ") + tui.StyleBody.Render(fmt.Sprintf("%d", m.queueDropped)) +
		tui.StyleMuted.Render("  Triggers/min: ") + tui.StyleBody.Render(fmt.Sprintf("%d", m.triggersLastMinute))
	uptimeLine := tui.StyleMuted.Render("Uptime: ") + tui.StyleBody.Render(uptime)
	if m.filterText != "" && !m.filterMode {
		uptimeLine += tui.StyleMuted.Render("  Filter: ") + tui.StyleBody.Render(m.filterText)
	}
	bannerRight = append(bannerRight, statusLine, uptimeLine)

	artStyle := lipgloss.NewStyle().Foreground(tui.ColorCrabOrange)
	for i, line := range art {
		styled := artStyle.Render(line)
		right := ""
		if i < len(bannerRight) {
			right = bannerRight[i]
		}
		lines = append(lines, styled+"   "+right)
	}

	// Divider
	lines = append(lines, "")
	lines = append(lines, tui.StyleDivider(m.width))

	// Event feed
	if len(m.feed) == 0 {
		lines = append(lines, tui.StyleMuted.Render("  (waiting for events...)"))
	} else {
		lines = append(lines, m.feed...)
	}

	// Filter bar
	if m.filterMode {
		lines = append(lines, "")
		lines = append(lines, m.filterInput.View())
	}

	// Fatal error
	if m.fatalErr != nil {
		lines = append(lines, "")
		lines = append(lines, tui.StyleError.Render("FATAL: "+m.fatalErr.Error()))
	}

	// Key help
	lines = append(lines, "")
	lines = append(lines, tui.StyleMuted.Render("  / filter  esc clear  q quit"))

	return strings.Join(lines, "\n")
}

func (m watchModel) connectionIndicator() string {
	if m.connected {
		return lipgloss.NewStyle().Foreground(tui.ColorSeafoam).Render("◉ connected")
	}
	if m.reconnectAttempts > 0 && m.fatalErr == nil {
		return lipgloss.NewStyle().Foreground(tui.ColorWarmGold).Render("○ reconnecting")
	}
	return lipgloss.NewStyle().Foreground(tui.ColorCoralRed).Render("✖ disconnected")
}

func (m *watchModel) handleStreamLine(line []byte) {
	var notif struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &notif); err != nil {
		return
	}

	switch notif.Method {
	case "audit.event":
		var evt audit.AuditEvent
		if err := json.Unmarshal(notif.Params, &evt); err != nil {
			return
		}
		m.recordAuditEvent(evt)
	case "audit.heartbeat":
		var heartbeat struct {
			QueueDepth   int    `json:"queue_depth"`
			QueueDropped uint64 `json:"queue_dropped"`
		}
		if err := json.Unmarshal(notif.Params, &heartbeat); err != nil {
			return
		}
		m.queueDepth = heartbeat.QueueDepth
		m.queueDropped = heartbeat.QueueDropped
	}
}

var (
	tsStyle    = lipgloss.NewStyle().Foreground(tui.ColorDriftGray)
	agentStyle = lipgloss.NewStyle().Foreground(tui.ColorWarmGold)
	bodyStyle  = lipgloss.NewStyle().Foreground(tui.ColorShellWhite)
	argStyle   = lipgloss.NewStyle().Foreground(tui.ColorDriftGray)
	warnLine   = lipgloss.NewStyle().Foreground(tui.ColorCrabOrange)
	blockLine  = lipgloss.NewStyle().Foreground(tui.ColorCoralRed)
)

func (m *watchModel) recordAuditEvent(evt audit.AuditEvent) {
	when := evt.Timestamp
	if when.IsZero() {
		when = m.deps.Now()
	}

	if isTriggeredAuditEvent(evt) {
		m.triggerTimes = append(m.triggerTimes, when)
	}
	windowStart := m.deps.Now().Add(-1 * time.Minute)
	kept := m.triggerTimes[:0]
	for _, ts := range m.triggerTimes {
		if !ts.Before(windowStart) {
			kept = append(kept, ts)
		}
	}
	m.triggerTimes = kept
	m.triggersLastMinute = len(m.triggerTimes)

	ts := tui.FormatTimestamp(when)
	args := tui.Truncate(evt.Arguments, 50)

	// Build styled line parts
	prefix := "  "
	line := tsStyle.Render(ts) + " " +
		agentStyle.Render("["+evt.AgentID+"]") + " " +
		bodyStyle.Render(fmt.Sprintf("%-18s", string(evt.ActionType))) + " " +
		bodyStyle.Render(fmt.Sprintf("%-10s", evt.Action))

	if args != "" {
		line += " " + argStyle.Render(args)
	}

	if evt.ActionType == audit.ActionAIRequest && evt.CostUSD > 0 {
		line += " " + argStyle.Render("("+tui.FormatCost(evt.CostUSD)+")")
	}

	switch evt.Outcome {
	case audit.OutcomeWarned:
		prefix = tui.StatusIcon("warned") + " "
		line = warnLine.Render(ts+" ["+evt.AgentID+"] "+fmt.Sprintf("%-18s", string(evt.ActionType))+" "+fmt.Sprintf("%-10s", evt.Action)) +
			warnLineDetail(args, evt)
	case audit.OutcomeBlocked:
		prefix = tui.StatusIcon("blocked") + " "
		line = blockLine.Render(ts+" ["+evt.AgentID+"] "+fmt.Sprintf("%-18s", string(evt.ActionType))+" "+fmt.Sprintf("%-10s", evt.Action)) +
			blockLineDetail(args, evt)
	}

	formatted := prefix + line

	searchable := strings.ToLower(strings.Join([]string{
		evt.AgentID,
		string(evt.ActionType),
		evt.Action,
		evt.Arguments,
		string(evt.Outcome),
		outcomeAliases(evt.Outcome),
	}, " "))

	entry := feedEntry{formatted: formatted, outcome: evt.Outcome, searchable: searchable}
	m.allEvents = append([]feedEntry{entry}, m.allEvents...)
	const maxAllEvents = 200
	if len(m.allEvents) > maxAllEvents {
		m.allEvents = m.allEvents[:maxAllEvents]
	}
	m.rebuildFeed()
}

func warnLineDetail(args string, evt audit.AuditEvent) string {
	detail := ""
	if args != "" {
		detail += " " + warnLine.Render(args)
	}
	if evt.ActionType == audit.ActionAIRequest && evt.CostUSD > 0 {
		detail += " " + warnLine.Render("("+tui.FormatCost(evt.CostUSD)+")")
	}
	return detail
}

func blockLineDetail(args string, evt audit.AuditEvent) string {
	detail := ""
	if args != "" {
		detail += " " + blockLine.Render(args)
	}
	if evt.ActionType == audit.ActionAIRequest && evt.CostUSD > 0 {
		detail += " " + blockLine.Render("("+tui.FormatCost(evt.CostUSD)+")")
	}
	return detail
}

func isTriggeredAuditEvent(evt audit.AuditEvent) bool {
	if evt.Outcome == audit.OutcomeWarned || evt.Outcome == audit.OutcomeBlocked {
		return true
	}

	raw := strings.TrimSpace(evt.CommandmentsTriggered)
	if raw == "" {
		return false
	}

	var triggered []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &triggered); err != nil {
		return false
	}

	return len(triggered) > 0
}

func (m *watchModel) appendFeed(line string) {
	m.feed = append([]string{line}, m.feed...)
	const maxFeed = 12
	if len(m.feed) > maxFeed {
		m.feed = m.feed[:maxFeed]
	}
}

func (m *watchModel) rebuildFeed() {
	m.feed = m.feed[:0]
	filter := strings.ToLower(m.filterText)
	for _, entry := range m.allEvents {
		if filter != "" && !strings.Contains(entry.searchable, filter) {
			continue
		}
		m.feed = append(m.feed, entry.formatted)
		if len(m.feed) >= 12 {
			break
		}
	}
}

func outcomeAliases(outcome audit.Outcome) string {
	switch outcome {
	case audit.OutcomeBlocked:
		return "block"
	case audit.OutcomeWarned:
		return "warn"
	default:
		return ""
	}
}

func readWatchStreamCmd(scanner *bufio.Scanner) tea.Cmd {
	return func() tea.Msg {
		if scanner == nil {
			return streamDisconnectedMsg{Err: io.EOF}
		}
		if scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			return watchStreamLineMsg{line: line}
		}
		err := scanner.Err()
		if err == nil {
			err = io.EOF
		}
		return streamDisconnectedMsg{Err: err}
	}
}

type auditEventMsg struct {
	Event audit.AuditEvent
}

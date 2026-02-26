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
)

var (
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange
	blockStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
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

	filterMode  bool
	filterInput textinput.Model
	filterText  string

	client  *ipc.Client
	scanner *bufio.Scanner
	deps    watchModelDeps
}

type feedEntry struct {
	formatted   string
	outcome     audit.Outcome
	searchable  string
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

	program := tea.NewProgram(m)
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
	return tea.Batch(cmds...)
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		m.appendFeed("reconnected")
		if m.scanner == nil {
			m.fatalErr = errors.New("watch stream reconnect returned no scanner")
			return m, tea.Quit
		}
		return m, readWatchStreamCmd(m.scanner)
	}

	return m, nil
}

func (m watchModel) View() string {
	title := lipgloss.NewStyle().Bold(true).Render("Crabwise Watch")
	uptime := m.daemonUptime
	if uptime == "" {
		uptime = m.deps.Now().Sub(m.startedAt).Round(time.Second).String()
	}
	status := fmt.Sprintf(
		"queue depth: %d | dropped: %d | uptime: %s | triggers/min: %d",
		m.queueDepth,
		m.queueDropped,
		uptime,
		m.triggersLastMinute,
	)
	if m.filterText != "" {
		status += fmt.Sprintf(" | Filter: %s", m.filterText)
	}

	if len(m.feed) == 0 {
		m.feed = []string{"(waiting for events...)"}
	}

	lines := append([]string{title, status, "", "Recent events:"}, m.feed...)
	if m.filterMode {
		lines = append(lines, "", m.filterInput.View())
	}
	if m.fatalErr != nil {
		lines = append(lines, "", "FATAL: "+m.fatalErr.Error())
	}
	lines = append(lines, "", "Press q to quit")

	return strings.Join(lines, "\n")
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

	ts := when.Format("15:04:05")
	line := fmt.Sprintf("%s [%s] %-18s %-10s %s", ts, evt.AgentID, evt.ActionType, evt.Action, truncate(evt.Arguments, 60))

	// Style and prefix based on outcome
	switch evt.Outcome {
	case audit.OutcomeWarned:
		line = warnStyle.Render("⚠ " + line)
	case audit.OutcomeBlocked:
		line = blockStyle.Render("✖ " + line)
	}

	searchable := strings.ToLower(strings.Join([]string{
		evt.AgentID,
		string(evt.ActionType),
		evt.Action,
		evt.Arguments,
		string(evt.Outcome),
		outcomeAliases(evt.Outcome),
	}, " "))

	entry := feedEntry{formatted: line, outcome: evt.Outcome, searchable: searchable}
	m.allEvents = append([]feedEntry{entry}, m.allEvents...)
	const maxAllEvents = 200
	if len(m.allEvents) > maxAllEvents {
		m.allEvents = m.allEvents[:maxAllEvents]
	}
	m.rebuildFeed()
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

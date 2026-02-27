package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/tui"
)

const defaultPollInterval = 3 * time.Second

// statusPollMsg carries the result of an IPC status poll.
type statusPollMsg struct {
	connected         bool
	uptime            string
	pid               interface{}
	agents            interface{}
	queueDepth        int
	queueDropped      uint64
	proxyReqs         interface{}
	proxyBlocked      interface{}
	proxyErrors       interface{}
	mappingDegraded   interface{}
	unclassifiedTools interface{}
	err               error
}

// statusTickMsg triggers the next poll cycle.
type statusTUITickMsg struct{}

type statusTUIModel struct {
	socketPath        string
	width             int
	connected         bool
	uptime            string
	pid               interface{}
	agents            interface{}
	queueDepth        int
	queueDropped      uint64
	proxyReqs         interface{}
	proxyBlocked      interface{}
	proxyErrors       interface{}
	mappingDegraded   interface{}
	unclassifiedTools interface{}
	otelEnabled       bool
	proxyListen       string
	logWatcherEnabled bool
	pollInterval      time.Duration
	err               error
}

func newStatusTUIModel(cfg *daemon.Config) statusTUIModel {
	return statusTUIModel{
		socketPath:        cfg.Daemon.SocketPath,
		width:             80,
		pollInterval:      defaultPollInterval,
		proxyListen:       cfg.Adapters.Proxy.Listen,
		logWatcherEnabled: cfg.Adapters.LogWatcher.Enabled,
		otelEnabled:       cfg.OTel.Enabled,
	}
}

func (m statusTUIModel) Init() tea.Cmd {
	return tea.Batch(
		pollStatus(m.socketPath),
		tea.Tick(m.pollInterval, func(time.Time) tea.Msg {
			return statusTUITickMsg{}
		}),
	)
}

func (m statusTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, pollStatus(m.socketPath)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width

	case statusPollMsg:
		if msg.err != nil {
			m.connected = false
			m.err = msg.err
		} else {
			m.connected = msg.connected
			m.err = nil
			m.uptime = msg.uptime
			m.pid = msg.pid
			m.agents = msg.agents
			m.queueDepth = msg.queueDepth
			m.queueDropped = msg.queueDropped
			m.proxyReqs = msg.proxyReqs
			m.proxyBlocked = msg.proxyBlocked
			m.proxyErrors = msg.proxyErrors
			m.mappingDegraded = msg.mappingDegraded
			m.unclassifiedTools = msg.unclassifiedTools
		}

	case statusTUITickMsg:
		return m, tea.Batch(
			pollStatus(m.socketPath),
			tea.Tick(m.pollInterval, func(time.Time) tea.Msg {
				return statusTUITickMsg{}
			}),
		)
	}

	return m, nil
}

func (m statusTUIModel) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	var b strings.Builder

	// Banner with "Status" heading
	b.WriteString(renderStatusBanner())
	b.WriteString("\n\n")

	if !m.connected {
		fmt.Fprintf(&b, "  %s %s\n", tui.StatusIcon("stopped"), tui.StyleBody.Render("Daemon not running"))
		b.WriteString("\n")
		b.WriteString(tui.RenderStatusBar("r retry  q quit", "", w))
		return b.String()
	}

	// Service status lines
	daemonStatus := "running"
	uptimeStr := ""
	if m.uptime != "" {
		uptimeStr = fmt.Sprintf("Uptime: %s", m.uptime)
	}
	agentsStr := ""
	if m.agents != nil {
		agentsStr = fmt.Sprintf("Agents: %v", m.agents)
	}
	fmt.Fprintf(&b, "  %s %s   %s\n",
		tui.StatusIcon(daemonStatus),
		padRight(tui.StyleBody.Render("Daemon"), tui.StyleMuted.Render("running"), 30),
		tui.StyleMuted.Render(uptimeStr),
	)

	// Log watcher
	if m.logWatcherEnabled {
		fmt.Fprintf(&b, "  %s %s   %s\n",
			tui.StatusIcon("active"),
			padRight(tui.StyleBody.Render("Log watcher"), tui.StyleMuted.Render("active"), 30),
			tui.StyleMuted.Render(agentsStr),
		)
	}

	// Proxy
	if m.proxyReqs != nil {
		proxyStatus := m.proxyListen
		reqsStr := fmt.Sprintf("Reqs: %v", m.proxyReqs)
		fmt.Fprintf(&b, "  %s %s   %s\n",
			tui.StatusIcon("running"),
			padRight(tui.StyleBody.Render("Proxy"), tui.StyleMuted.Render(proxyStatus), 30),
			tui.StyleMuted.Render(reqsStr),
		)
	}

	// OTel
	if m.otelEnabled {
		fmt.Fprintf(&b, "  %s %s\n",
			tui.StatusIcon("active"),
			padRight(tui.StyleBody.Render("OTel"), tui.StyleMuted.Render("enabled"), 30),
		)
	} else {
		fmt.Fprintf(&b, "  %s %s\n",
			tui.StatusIcon("stopped"),
			padRight(tui.StyleBody.Render("OTel"), tui.StyleMuted.Render("disabled"), 30),
		)
	}

	b.WriteString("\n")

	// Queue section
	b.WriteString(sectionDivider("Queue", w))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "  %s %s            %s %s\n",
		tui.StyleMuted.Render("Depth:"),
		tui.StyleBody.Render(fmt.Sprintf("%d / 10,000", m.queueDepth)),
		tui.StyleMuted.Render("Dropped:"),
		tui.StyleBody.Render(fmt.Sprintf("%d", m.queueDropped)),
	)

	b.WriteString("\n")

	// Proxy section
	if m.proxyReqs != nil {
		b.WriteString(sectionDivider("Proxy", w))
		b.WriteString("\n\n")
		fmt.Fprintf(&b, "  %s %s      %s %s      %s %s\n",
			tui.StyleMuted.Render("Total:"),
			tui.StyleBody.Render(fmt.Sprintf("%v", m.proxyReqs)),
			tui.StyleMuted.Render("Blocked:"),
			tui.StyleBody.Render(fmt.Sprintf("%v", m.proxyBlocked)),
			tui.StyleMuted.Render("Errors:"),
			tui.StyleBody.Render(fmt.Sprintf("%v", m.proxyErrors)),
		)
		fmt.Fprintf(&b, "  %s %s        %s %s\n",
			tui.StyleMuted.Render("Degraded:"),
			tui.StyleBody.Render(fmt.Sprintf("%v", m.mappingDegraded)),
			tui.StyleMuted.Render("Unclassified:"),
			tui.StyleBody.Render(fmt.Sprintf("%v", m.unclassifiedTools)),
		)
		b.WriteString("\n")
	}

	// Status bar
	pollSec := fmt.Sprintf("%ds", int(m.pollInterval.Seconds()))
	b.WriteString(tui.RenderStatusBar("r refresh  q quit", pollSec, w))

	return b.String()
}

// renderStatusBanner renders the crab art with "Status" as the heading.
func renderStatusBanner() string {
	art := tui.CrabArt
	gap := "  "
	rightText := []string{
		tui.StyleHeading.Render("Status"),
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

// sectionDivider renders " ─═─ Name ─═─═─…" spanning width.
func sectionDivider(name string, width int) string {
	prefix := " ─═─ "
	suffix := " "
	label := prefix + name + suffix
	remaining := width - len([]rune(label))
	if remaining < 0 {
		remaining = 0
	}
	fill := tui.StyleDivider(remaining)
	return tui.StyleMuted.Render(label) + fill
}

// padRight renders label and value with padding between.
func padRight(label, value string, totalWidth int) string {
	labelWidth := lipgloss.Width(label)
	valueWidth := lipgloss.Width(value)
	gap := totalWidth - labelWidth - valueWidth
	if gap < 1 {
		gap = 1
	}
	return label + strings.Repeat(" ", gap) + value
}

// pollStatus performs an IPC call to the daemon and returns a statusPollMsg.
func pollStatus(socketPath string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			return statusPollMsg{connected: false, err: nil}
		}
		defer client.Close()

		result, err := client.Call("status", nil)
		if err != nil {
			return statusPollMsg{connected: false, err: nil}
		}

		var status map[string]interface{}
		if err := json.Unmarshal(result, &status); err != nil {
			return statusPollMsg{connected: false, err: err}
		}

		msg := statusPollMsg{
			connected: true,
		}
		if v, ok := status["uptime"]; ok {
			msg.uptime = fmt.Sprintf("%v", v)
		}
		msg.pid = status["pid"]
		msg.agents = status["agents"]

		if v, ok := status["queue_depth"]; ok {
			if f, ok := v.(float64); ok {
				msg.queueDepth = int(f)
			}
		}
		if v, ok := status["queue_dropped"]; ok {
			if f, ok := v.(float64); ok {
				msg.queueDropped = uint64(f)
			}
		}

		msg.proxyReqs = status["proxy_requests_total"]
		msg.proxyBlocked = status["proxy_blocked_total"]
		msg.proxyErrors = status["proxy_upstream_errors"]
		msg.mappingDegraded = status["mapping_degraded_count"]
		msg.unclassifiedTools = status["unclassified_tool_count"]

		return msg
	}
}

func runStatusTUI(cfg *daemon.Config) error {
	m := newStatusTUIModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

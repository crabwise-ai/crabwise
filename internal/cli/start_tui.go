package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crabwise-ai/crabwise/internal/certs"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/tui"
)

type startState string

const (
	startStateStarting startState = "starting"
	startStateRunning  startState = "running"
	startStateStopping startState = "stopping"
	startStateStopped  startState = "stopped"
	startStateWarning  startState = "warning"
	startStateError    startState = "error"
	startStateDisabled startState = "disabled"
)

type startEventLevel string

const (
	startEventInfo  startEventLevel = "info"
	startEventWarn  startEventLevel = "warn"
	startEventError startEventLevel = "error"
)

type startEvent struct {
	when   time.Time
	level  startEventLevel
	detail string
}

type startLogMsg struct {
	line string
}

type startTickMsg struct{}

type daemonExitMsg struct {
	err error
}

type startTUIDeps struct {
	Now    func() time.Time
	Cancel func()
}

type startTUIModel struct {
	cfg   *daemon.Config
	deps  startTUIDeps
	width int

	startedAt time.Time
	uptime    string

	spinner spinner.Model

	daemonState     startState
	logWatcherState startState
	proxyState      startState

	proxyDetail string
	proxyNote   string

	trustPanel string

	events       []startEvent
	shutdownNote string
	daemonRunErr error
}

type logLineWriter struct {
	mu  sync.Mutex
	buf string
	ch  chan string
}

func (w *logLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf += string(p)
	for {
		idx := strings.IndexByte(w.buf, '\n')
		if idx == -1 {
			break
		}
		line := strings.TrimRight(w.buf[:idx], "\r")
		w.buf = w.buf[idx+1:]
		w.send(line)
	}

	return len(p), nil
}

func (w *logLineWriter) send(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	select {
	case w.ch <- line:
	default:
	}
}

func runStartTUI(cfg *daemon.Config) error {
	deps := startTUIDeps{
		Now:    time.Now,
		Cancel: func() {},
	}

	ctx, cancel := context.WithCancel(context.Background())
	deps.Cancel = cancel

	model := newStartTUIModel(cfg, deps)
	program := tea.NewProgram(model, tea.WithAltScreen())

	logCh := make(chan string, 128)
	logWriter := &logLineWriter{ch: logCh}
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(logWriter)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	}()

	go func() {
		for line := range logCh {
			program.Send(startLogMsg{line: line})
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		daemon.Version = Version
		d := daemon.New(cfg, "")
		errCh <- d.Run(ctx)
	}()
	go func() {
		err := <-errCh
		program.Send(daemonExitMsg{err: err})
	}()

	finalModel, err := program.Run()
	if err != nil {
		return err
	}
	final := finalModel.(startTUIModel)
	if final.daemonRunErr != nil {
		return final.daemonRunErr
	}
	return nil
}

func newStartTUIModel(cfg *daemon.Config, deps startTUIDeps) startTUIModel {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	sp := tui.NewSpinner(tui.SpinnerLine)

	now := deps.Now()
	model := startTUIModel{
		cfg:         cfg,
		deps:        deps,
		width:       80,
		startedAt:   now,
		uptime:      "0s",
		spinner:     sp,
		proxyNote:   "",
		proxyDetail: "",
		events:      make([]startEvent, 0, 8),
	}

	model.daemonState = startStateStarting
	if cfg.Adapters.LogWatcher.Enabled {
		model.logWatcherState = startStateStarting
	} else {
		model.logWatcherState = startStateDisabled
	}
	if cfg.Adapters.Proxy.Enabled {
		model.proxyState = startStateStarting
		model.proxyDetail = cfg.Adapters.Proxy.Listen
	} else {
		model.proxyState = startStateDisabled
		model.proxyDetail = "disabled"
	}

	model.trustPanel, model.proxyNote, model.proxyState = renderTrustPanel(cfg)

	model.addEvent(startEventInfo, "Starting daemon")

	return model
}

func (m startTUIModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tea.Tick(time.Second, func(time.Time) tea.Msg {
			return startTickMsg{}
		}),
	)
}

func (m startTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.deps.Cancel != nil {
				m.deps.Cancel()
			}
			if m.daemonState == startStateRunning || m.daemonState == startStateStarting {
				m.daemonState = startStateStopping
			}
			if m.shutdownNote == "" {
				m.shutdownNote = "stop requested"
				m.addEvent(startEventWarn, "Shutdown requested")
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case startTickMsg:
		m.uptime = tui.FormatDuration(m.deps.Now().Sub(m.startedAt))
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
			return startTickMsg{}
		})

	case startLogMsg:
		m.handleLogLine(msg.line)
		return m, nil

	case daemonExitMsg:
		m.daemonRunErr = msg.err
		if msg.err != nil {
			m.daemonState = startStateError
			m.addEvent(startEventError, "Daemon exited: "+msg.err.Error())
		} else {
			m.daemonState = startStateStopped
			if m.shutdownNote == "" {
				m.shutdownNote = "stopped"
			}
			m.addEvent(startEventInfo, "Daemon stopped")
		}
		return m, tea.Quit
	}

	return m, nil
}

func (m startTUIModel) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	var b strings.Builder
	b.WriteString(renderStartBanner(Version))
	b.WriteString("\n\n")

	b.WriteString(sectionDivider("Services", w))
	b.WriteString("\n\n")

	daemonDetail := stateLabel(m.daemonState)
	daemonRight := ""
	if m.daemonState == startStateRunning || m.daemonState == startStateStarting || m.daemonState == startStateStopping {
		daemonRight = "Uptime: " + m.uptime
	}
	fmt.Fprintf(&b, "  %s %s   %s\n",
		m.stateIcon(m.daemonState),
		padRight(tui.StyleBody.Render("Daemon"), tui.StyleMuted.Render(daemonDetail), 30),
		tui.StyleMuted.Render(daemonRight),
	)

	logWatcherDetail := "disabled"
	if m.logWatcherState != startStateDisabled {
		logWatcherDetail = "enabled"
	}
	fmt.Fprintf(&b, "  %s %s\n",
		m.stateIcon(m.logWatcherState),
		padRight(tui.StyleBody.Render("Log watcher"), tui.StyleMuted.Render(logWatcherDetail), 30),
	)

	proxyDetail := m.proxyDetail
	if proxyDetail == "" {
		proxyDetail = "disabled"
	}
	fmt.Fprintf(&b, "  %s %s   %s\n",
		m.stateIcon(m.proxyState),
		padRight(tui.StyleBody.Render("Proxy"), tui.StyleMuted.Render(proxyDetail), 30),
		tui.StyleMuted.Render(m.proxyNote),
	)

	b.WriteString("\n")
	b.WriteString(sectionDivider("Paths", w))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "  %s %s\n",
		tui.StyleMuted.Render("Socket:"),
		tui.StyleBody.Render(cfgPath(m.cfg.Daemon.SocketPath)),
	)
	fmt.Fprintf(&b, "  %s %s\n",
		tui.StyleMuted.Render("DB:"),
		tui.StyleBody.Render(cfgPath(m.cfg.Daemon.DBPath)),
	)
	fmt.Fprintf(&b, "  %s %s\n",
		tui.StyleMuted.Render("PID:"),
		tui.StyleBody.Render(fmt.Sprintf("%d", os.Getpid())),
	)

	if len(m.events) > 0 {
		b.WriteString("\n")
		b.WriteString(sectionDivider("Lifecycle", w))
		b.WriteString("\n\n")
		for _, evt := range m.events {
			fmt.Fprintf(&b, "  %s %s\n",
				tui.StyleMuted.Render(tui.FormatTimestamp(evt.when)),
				renderEvent(evt),
			)
		}
	}

	if m.trustPanel != "" {
		b.WriteString("\n")
		b.WriteString(m.trustPanel)
	}

	b.WriteString("\n\n")
	b.WriteString(tui.RenderStatusBar("q quit  ctrl+c stop", "", w))

	return b.String()
}

func (m *startTUIModel) handleLogLine(line string) {
	switch {
	case strings.HasPrefix(line, "daemon: started"):
		m.daemonState = startStateRunning
		if m.logWatcherState == startStateStarting {
			m.logWatcherState = startStateRunning
		}
		if m.proxyState == startStateStarting {
			m.proxyState = startStateRunning
		}
		m.addEvent(startEventInfo, "Daemon started")
	case strings.HasPrefix(line, "daemon: received "):
		note := strings.TrimPrefix(line, "daemon: received ")
		note = strings.TrimSuffix(note, ", shutting down")
		if note == "" {
			note = "signal"
		}
		m.shutdownNote = note
		m.daemonState = startStateStopping
		m.addEvent(startEventWarn, "Shutdown requested ("+note+")")
	case strings.HasPrefix(line, "daemon: context cancelled"):
		m.shutdownNote = "context cancelled"
		m.daemonState = startStateStopping
		m.addEvent(startEventWarn, "Shutdown requested (context cancelled)")
	case strings.HasPrefix(line, "daemon: log watcher start error:"):
		m.logWatcherState = startStateError
		m.addEvent(startEventError, strings.TrimPrefix(line, "daemon: "))
	case strings.HasPrefix(line, "daemon: proxy init error:"):
		m.proxyState = startStateError
		m.addEvent(startEventError, strings.TrimPrefix(line, "daemon: "))
	case strings.HasPrefix(line, "daemon: proxy server error:"):
		m.proxyState = startStateError
		m.addEvent(startEventError, strings.TrimPrefix(line, "daemon: "))
	default:
		if strings.Contains(strings.ToLower(line), "error") {
			m.addEvent(startEventWarn, line)
		}
	}
}

func (m *startTUIModel) addEvent(level startEventLevel, detail string) {
	entry := startEvent{
		when:   m.deps.Now(),
		level:  level,
		detail: detail,
	}
	m.events = append([]startEvent{entry}, m.events...)
	const maxEvents = 6
	if len(m.events) > maxEvents {
		m.events = m.events[:maxEvents]
	}
}

func renderEvent(evt startEvent) string {
	switch evt.level {
	case startEventError:
		return tui.StyleError.Render(evt.detail)
	case startEventWarn:
		return tui.StyleWarning.Render(evt.detail)
	default:
		return tui.StyleBody.Render(evt.detail)
	}
}

func (m startTUIModel) stateIcon(state startState) string {
	switch state {
	case startStateStarting, startStateStopping:
		return m.spinner.View()
	case startStateRunning:
		return tui.StatusIcon("running")
	case startStateWarning:
		return tui.StatusIcon("warning")
	case startStateError:
		return tui.StatusIcon("error")
	case startStateStopped, startStateDisabled:
		return tui.StatusIcon("stopped")
	default:
		return " "
	}
}

func stateLabel(state startState) string {
	switch state {
	case startStateStarting:
		return "starting"
	case startStateRunning:
		return "running"
	case startStateStopping:
		return "stopping"
	case startStateWarning:
		return "warning"
	case startStateError:
		return "error"
	case startStateDisabled:
		return "disabled"
	default:
		return "stopped"
	}
}

func renderTrustPanel(cfg *daemon.Config) (string, string, startState) {
	if !cfg.Adapters.Proxy.Enabled {
		return "", "", startStateDisabled
	}

	status := certs.CheckTrust(cfg.Adapters.Proxy.CACert, cfg.Adapters.Proxy.CAKey)
	if !status.Exists {
		body := "Crabwise proxy is enabled but the CA cert/key files are missing.\n\n" +
			"Run:\ncrabwise cert trust"
		return tui.RenderPanel("Action required: Generate the CA", body), "CA missing", startStateWarning
	}
	if status.Trusted {
		return "", "", startStateStarting
	}

	commands := certs.CommandsForOS(cfg.Adapters.Proxy.CACert)
	if commands.SystemTrustCmd != "" {
		body := "Crabwise is configured to intercept HTTPS (MITM).\n\n" +
			"Copy/paste:\n" + commands.SystemTrustCmd + "\n\n" +
			"Or copy it:\ncrabwise cert trust --copy"
		return tui.RenderPanel("Action required: Trust the CA ("+commands.OS+")", body), "trust required", startStateWarning
	}

	body := "Crabwise is configured to intercept HTTPS (MITM).\n\n" +
		"Manually add this CA to your system trust store:\n" + status.CertPath
	return tui.RenderPanel("Action required: Trust the CA", body), "trust required", startStateWarning
}

func renderStartBanner(version string) string {
	art := tui.CrabArt
	gap := "  "
	rightText := []string{
		tui.StyleHeading.Render("Start Daemon"),
		tui.StyleMuted.Render("Crabwise v" + version),
		tui.StyleDivider(27),
		tui.StyleMuted.Render("Foreground daemon"),
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

func cfgPath(path string) string {
	return strings.ReplaceAll(path, "\n", "")
}

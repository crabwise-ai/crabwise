package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"gopkg.in/yaml.v3"
)

// settingKind determines how a setting row is edited.
type settingKind int

const (
	kindBool settingKind = iota
	kindString
	kindDuration
	kindSection // non-editable section header
)

type settingItem struct {
	key     string // YAML path for display (e.g. "notifications.desktop.enabled")
	label   string
	kind    settingKind
	section string // group label
}

// All editable settings. Extend this list to expose more config fields.
var settingsSchema = []settingItem{
	{key: "_section_desktop", label: "Desktop Notifications", kind: kindSection, section: "desktop"},
	{key: "notifications.desktop.enabled", label: "Enabled", kind: kindBool, section: "desktop"},
	{key: "notifications.desktop.min_interval", label: "Min Interval", kind: kindDuration, section: "desktop"},

	{key: "_section_webhook", label: "Webhook Notifications", kind: kindSection, section: "webhook"},
	{key: "notifications.webhook.enabled", label: "Enabled", kind: kindBool, section: "webhook"},
	{key: "notifications.webhook.url", label: "URL", kind: kindString, section: "webhook"},
	{key: "notifications.webhook.auth_header_env", label: "Auth Header Env", kind: kindString, section: "webhook"},
	{key: "notifications.webhook.format", label: "Format", kind: kindString, section: "webhook"},
	{key: "notifications.webhook.min_interval", label: "Min Interval", kind: kindDuration, section: "webhook"},

	{key: "_section_audit", label: "Audit", kind: kindSection, section: "audit"},
	{key: "audit.retention_days", label: "Retention Days", kind: kindString, section: "audit"},
	{key: "audit.raw_payload_enabled", label: "Raw Payload Enabled", kind: kindBool, section: "audit"},

	{key: "_section_daemon", label: "Daemon", kind: kindSection, section: "daemon"},
	{key: "daemon.log_level", label: "Log Level", kind: kindString, section: "daemon"},
}

type settingsModel struct {
	cfg     *daemon.Config
	cfgPath string

	items    []settingItem
	cursor   int
	editing  bool
	input    textinput.Model
	dirty    bool
	saved    bool
	saveMsg  string
	saveTick int
	width    int
	height   int
}

func runSettingsTUI(cfg *daemon.Config, cfgPath string) error {
	m := newSettingsModel(cfg, cfgPath)
	program := tea.NewProgram(m, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func newSettingsModel(cfg *daemon.Config, cfgPath string) settingsModel {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 50

	m := settingsModel{
		cfg:     cfg,
		cfgPath: cfgPath,
		items:   settingsSchema,
		width:   80,
		height:  24,
		input:   ti,
	}
	// Start cursor on first editable item
	m.cursor = m.nextEditable(-1)
	return m
}

func (m settingsModel) Init() tea.Cmd { return nil }

func (m settingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case settingsSaveTickMsg:
		m.saveTick--
		if m.saveTick <= 0 {
			m.saveMsg = ""
			return m, nil
		}
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return settingsSaveTickMsg{} })

	case tea.KeyMsg:
		if m.editing {
			return m.updateEditing(msg)
		}
		return m.updateNavigating(msg)
	}

	return m, nil
}

func (m settingsModel) updateNavigating(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.cursor = m.prevEditable(m.cursor)
	case "down", "j":
		m.cursor = m.nextEditable(m.cursor)
	case " ", "enter":
		item := m.items[m.cursor]
		if item.kind == kindSection {
			return m, nil
		}
		if item.kind == kindBool {
			m.toggleBool(item.key)
			m.dirty = true
			return m, nil
		}
		// Start editing string/duration
		m.editing = true
		m.input.SetValue(m.getValue(item.key))
		m.input.Focus()
		m.input.CursorEnd()
		return m, m.input.Cursor.BlinkCmd()
	case "s":
		return m.save()
	}
	return m, nil
}

func (m settingsModel) updateEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		item := m.items[m.cursor]
		val := m.input.Value()
		if err := m.setValue(item.key, val); err != nil {
			m.saveMsg = tui.StyleError.Render("invalid: " + err.Error())
			m.saveTick = 3
			m.editing = false
			m.input.Blur()
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return settingsSaveTickMsg{} })
		}
		m.dirty = true
		m.editing = false
		m.input.Blur()
		return m, nil
	case "esc":
		m.editing = false
		m.input.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

type settingsSaveTickMsg struct{}

func (m settingsModel) save() (tea.Model, tea.Cmd) {
	if err := m.cfg.Validate(); err != nil {
		m.saveMsg = tui.StyleError.Render("validation: " + err.Error())
		m.saveTick = 3
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return settingsSaveTickMsg{} })
	}

	data, err := yaml.Marshal(m.cfg)
	if err != nil {
		m.saveMsg = tui.StyleError.Render("marshal: " + err.Error())
		m.saveTick = 3
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return settingsSaveTickMsg{} })
	}

	dir := m.cfgPath[:strings.LastIndex(m.cfgPath, "/")]
	if dir != "" {
		os.MkdirAll(dir, 0700)
	}

	if err := os.WriteFile(m.cfgPath, data, 0600); err != nil {
		m.saveMsg = tui.StyleError.Render("write: " + err.Error())
		m.saveTick = 3
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return settingsSaveTickMsg{} })
	}

	m.dirty = false
	m.saved = true
	m.saveMsg = tui.StyleSuccess.Render("saved to " + m.cfgPath)
	m.saveTick = 3
	return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return settingsSaveTickMsg{} })
}

func (m settingsModel) View() string {
	var b strings.Builder

	// Banner
	artStyle := lipgloss.NewStyle().Foreground(tui.ColorCrabOrange)
	bannerRight := []string{
		tui.StyleHeading.Render("Settings"),
		tui.StyleDivider(27),
		tui.StyleMuted.Render("Config: ") + tui.StyleBody.Render(m.cfgPath),
	}
	if m.dirty {
		bannerRight = append(bannerRight, tui.StyleWarning.Render("● unsaved changes"))
	} else if m.saved {
		bannerRight = append(bannerRight, tui.StyleSuccess.Render("● saved"))
	}

	for i, art := range tui.CrabArt {
		styled := artStyle.Render(art)
		right := ""
		if i < len(bannerRight) {
			right = bannerRight[i]
		}
		b.WriteString(styled + "   " + right + "\n")
	}

	b.WriteString("\n")
	b.WriteString(tui.StyleDivider(m.width) + "\n")
	b.WriteString("\n")

	// Settings list
	cursorStyle := lipgloss.NewStyle().Foreground(tui.ColorWarmGold).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(tui.ColorShellWhite).Width(20)
	valStyle := lipgloss.NewStyle().Foreground(tui.ColorSeafoam)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(tui.ColorCrabOrange).MarginTop(1)

	for i, item := range m.items {
		if item.kind == kindSection {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("  " + sectionStyle.Render(item.label) + "\n")
			continue
		}

		prefix := "  "
		if i == m.cursor {
			prefix = cursorStyle.Render("▸ ")
		}

		val := m.getValue(item.key)
		valRendered := valStyle.Render(val)
		if item.kind == kindBool {
			if val == "true" {
				valRendered = tui.StyleSuccess.Render("● on")
			} else {
				valRendered = tui.StyleMuted.Render("○ off")
			}
		}

		if i == m.cursor && m.editing {
			valRendered = m.input.View()
		}

		b.WriteString(prefix + labelStyle.Render(item.label) + " " + valRendered + "\n")
	}

	// Save message
	if m.saveMsg != "" {
		b.WriteString("\n  " + m.saveMsg + "\n")
	}

	// Status bar
	b.WriteString("\n")
	hints := "↑↓ navigate  space toggle  enter edit  s save  q quit"
	if m.editing {
		hints = "enter confirm  esc cancel"
	}
	b.WriteString(tui.RenderStatusBar(hints, "", m.width))

	return b.String()
}

// Navigation helpers — skip section headers.
func (m settingsModel) nextEditable(from int) int {
	for i := from + 1; i < len(m.items); i++ {
		if m.items[i].kind != kindSection {
			return i
		}
	}
	return from
}

func (m settingsModel) prevEditable(from int) int {
	for i := from - 1; i >= 0; i-- {
		if m.items[i].kind != kindSection {
			return i
		}
	}
	return from
}

// getValue reads a config field by dot-path.
func (m settingsModel) getValue(key string) string {
	switch key {
	case "notifications.desktop.enabled":
		return fmt.Sprintf("%v", m.cfg.Notifications.Desktop.Enabled)
	case "notifications.desktop.min_interval":
		return m.cfg.Notifications.Desktop.MinInterval.Duration().String()
	case "notifications.webhook.enabled":
		return fmt.Sprintf("%v", m.cfg.Notifications.Webhook.Enabled)
	case "notifications.webhook.url":
		return m.cfg.Notifications.Webhook.URL
	case "notifications.webhook.auth_header_env":
		return m.cfg.Notifications.Webhook.AuthHeaderEnv
	case "notifications.webhook.format":
		return m.cfg.Notifications.Webhook.Format
	case "notifications.webhook.min_interval":
		return m.cfg.Notifications.Webhook.MinInterval.Duration().String()
	case "audit.retention_days":
		return strconv.Itoa(m.cfg.Audit.RetentionDays)
	case "audit.raw_payload_enabled":
		return fmt.Sprintf("%v", m.cfg.Audit.RawPayloadEnabled)
	case "daemon.log_level":
		return m.cfg.Daemon.LogLevel
	default:
		return ""
	}
}

// setValue writes a config field by dot-path.
func (m *settingsModel) setValue(key, val string) error {
	switch key {
	case "notifications.desktop.min_interval", "notifications.webhook.min_interval":
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid duration: %w", err)
		}
		if key == "notifications.desktop.min_interval" {
			m.cfg.Notifications.Desktop.MinInterval = daemon.Duration(d)
		} else {
			m.cfg.Notifications.Webhook.MinInterval = daemon.Duration(d)
		}
	case "notifications.webhook.url":
		m.cfg.Notifications.Webhook.URL = val
	case "notifications.webhook.auth_header_env":
		m.cfg.Notifications.Webhook.AuthHeaderEnv = val
	case "notifications.webhook.format":
		switch val {
		case "", "discord":
		default:
			return fmt.Errorf("must be \"\" or \"discord\"")
		}
		m.cfg.Notifications.Webhook.Format = val
	case "audit.retention_days":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 {
			return fmt.Errorf("must be positive integer")
		}
		m.cfg.Audit.RetentionDays = n
	case "daemon.log_level":
		switch val {
		case "debug", "info", "warn", "error":
		default:
			return fmt.Errorf("must be debug|info|warn|error")
		}
		m.cfg.Daemon.LogLevel = val
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	return nil
}

// toggleBool flips a boolean config field.
func (m *settingsModel) toggleBool(key string) {
	switch key {
	case "notifications.desktop.enabled":
		m.cfg.Notifications.Desktop.Enabled = !m.cfg.Notifications.Desktop.Enabled
	case "notifications.webhook.enabled":
		m.cfg.Notifications.Webhook.Enabled = !m.cfg.Notifications.Webhook.Enabled
	case "audit.raw_payload_enabled":
		m.cfg.Audit.RawPayloadEnabled = !m.cfg.Audit.RawPayloadEnabled
	}
}

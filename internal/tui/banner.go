package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CrabArt holds the raw crab ASCII art lines for reuse in custom banners.
// Compact 5×10 version (downsampled from 10×20).
var CrabArt = []string{
	`▄█▀    ▀█▄`,
	`▓   ▓ ▓  ▓`,
	`▀▓▓▓▓▓▓▓▓▀`,
	`█  ▓  ▓  █`,
}

const (
	bannerTickInterval = 60 * time.Millisecond
	bannerGap          = "   " // gap between art and text
)

// BannerDone is sent when the wave animation completes.
type BannerDone struct{}

type bannerTickMsg struct{}

// BannerModel implements tea.Model for the animated crab banner.
type BannerModel struct {
	version string
	pos     int // wave front position (in runes across a line)
	maxPos  int // rune width of the widest art line + wave width
	done    bool
}

// NewBannerModel creates a new animated crab banner.
func NewBannerModel(version string) BannerModel {
	maxWidth := 0
	for _, line := range CrabArt {
		w := len([]rune(line))
		if w > maxWidth {
			maxWidth = w
		}
	}
	return BannerModel{
		version: version,
		pos:     0,
		maxPos:  maxWidth + 3, // 3-char wave width
	}
}

func (m BannerModel) Init() tea.Cmd {
	return tea.Tick(bannerTickInterval, func(time.Time) tea.Msg {
		return bannerTickMsg{}
	})
}

func (m BannerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(bannerTickMsg); ok {
		if m.done {
			return m, nil
		}
		m.pos++
		if m.pos >= m.maxPos {
			m.done = true
			return m, func() tea.Msg { return BannerDone{} }
		}
		return m, tea.Tick(bannerTickInterval, func(time.Time) tea.Msg {
			return bannerTickMsg{}
		})
	}
	return m, nil
}

func (m BannerModel) View() string {
	rightText := bannerRightText(m.version)
	var lines []string
	for i, art := range CrabArt {
		rendered := m.renderArtLine(art)
		right := ""
		if i < len(rightText) {
			right = rightText[i]
		}
		lines = append(lines, rendered+bannerGap+right)
	}
	return strings.Join(lines, "\n")
}

func (m BannerModel) renderArtLine(line string) string {
	runes := []rune(line)
	var b strings.Builder
	for i, r := range runes {
		var style lipgloss.Style
		switch {
		case m.done || i < m.pos-2:
			style = lipgloss.NewStyle().Foreground(ColorCrabOrange)
		case i == m.pos-2:
			style = lipgloss.NewStyle().Foreground(ColorWarmGold)
		default:
			style = lipgloss.NewStyle().Foreground(ColorDriftGray)
		}
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}

func bannerRightText(version string) []string {
	return []string{
		StyleBody.Render(fmt.Sprintf("Crabwise AI v%s", version)),
		StyleBody.Render("Local-first AI agent governance"),
		StyleDivider(27),
		StyleBody.Render("https://github.com/crabwise-ai/crabwise"),
	}
}

// RenderBannerStatic returns the banner without animation for plain/non-interactive output.
func RenderBannerStatic(version string) string {
	rightText := bannerRightText(version)
	var lines []string
	for i, art := range CrabArt {
		styled := lipgloss.NewStyle().Foreground(ColorCrabOrange).Render(art)
		right := ""
		if i < len(rightText) {
			right = rightText[i]
		}
		lines = append(lines, styled+bannerGap+right)
	}
	return strings.Join(lines, "\n")
}

package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CrabArt holds the raw crab ASCII art lines for reuse in custom banners.
// Compact 4×10 version.
var CrabArt = []string{
	` ▄█▀    ▀█▄`,
	` █ ▂▂█▂█▂ █`,
	` ▀▓▓▓▓▓▓▓▓▀`,
	` █▀▀█▀▀█▀▀█`,
}

const (
	// BannerTickInterval is the ripple animation frame rate (slower = more visible wave).
	BannerTickInterval = 150 * time.Millisecond
	bannerGap          = "   " // gap between art and text
)

// BannerDone is sent when the wave animation completes (used by commands that run banner briefly).
type BannerDone struct{}

type bannerTickMsg struct{}

// rippleColors: sequential wave gradient (dim → crest → bright).
// Extra steps make the wave sweep more visible.
var rippleColors = []lipgloss.Color{
	ColorDriftGray,
	ColorDriftGray,
	ColorWarmGold,
	ColorWarmGold,
	ColorCrabOrange,
}

// blockChars are runes in CrabArt that get the color ripple (shape preserved).
var blockChars = map[rune]bool{
	'▄': true, '█': true, '▀': true, '▂': true, '▓': true,
}

// BannerModel implements tea.Model for the animated crab banner.
type BannerModel struct {
	version string
	tick    int // frame counter for ripple (loops via modulo)
}

// NewBannerModel creates a new animated crab banner with looping ripple effect.
func NewBannerModel(version string) BannerModel {
	return BannerModel{
		version: version,
		tick:    0,
	}
}

func (m BannerModel) Init() tea.Cmd {
	return tea.Tick(BannerTickInterval, func(time.Time) tea.Msg {
		return bannerTickMsg{}
	})
}

func (m BannerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(bannerTickMsg); ok {
		m.tick++
		return m, tea.Tick(BannerTickInterval, func(time.Time) tea.Msg {
			return bannerTickMsg{}
		})
	}
	return m, nil
}

func (m BannerModel) View() string {
	rightText := bannerRightText(m.version)
	var lines []string
	pos := 0
	for i, art := range CrabArt {
		rendered, nextPos := m.renderArtLine(art, pos)
		pos = nextPos
		right := ""
		if i < len(rightText) {
			right = rightText[i]
		}
		lines = append(lines, rendered+bannerGap+right)
	}
	return strings.Join(lines, "\n")
}

func (m BannerModel) renderArtLine(line string, startPos int) (string, int) {
	runes := []rune(line)
	var b strings.Builder
	pos := startPos
	for _, r := range runes {
		if blockChars[r] {
			phase := (m.tick + pos) % len(rippleColors)
			style := lipgloss.NewStyle().Foreground(rippleColors[phase])
			b.WriteString(style.Render(string(r)))
			pos++
		} else {
			style := lipgloss.NewStyle().Foreground(ColorCrabOrange)
			b.WriteString(style.Render(string(r)))
		}
	}
	return b.String(), pos
}

func bannerRightText(version string) []string {
	return []string{
		StyleBody.Render(fmt.Sprintf("Crabwise AI v%s", version)),
		StyleBody.Render("Local-first AI agent governance"),
		StyleDivider(27),
		StyleBody.Render("https://github.com/crabwise-ai/crabwise"),
	}
}

// CrabArtRippleStyled returns CrabArt with color ripple applied (shape preserved).
// Block characters keep their shape (▄ █ ▀ ▂ ▓) but cycle through DriftGray → WarmGold → CrabOrange.
// Returns styled strings ready to concatenate with right text.
func CrabArtRippleStyled(tick int) []string {
	result := make([]string, len(CrabArt))
	pos := 0
	for i, line := range CrabArt {
		runes := []rune(line)
		var b strings.Builder
		for _, r := range runes {
			if blockChars[r] {
				phase := (tick + pos) % len(rippleColors)
				style := lipgloss.NewStyle().Foreground(rippleColors[phase])
				b.WriteString(style.Render(string(r)))
				pos++
			} else {
				style := lipgloss.NewStyle().Foreground(ColorCrabOrange)
				b.WriteString(style.Render(string(r)))
			}
		}
		result[i] = b.String()
	}
	return result
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

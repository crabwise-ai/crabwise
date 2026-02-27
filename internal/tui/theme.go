package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Color palette — crustacean-futurist theme.
const (
	ColorCrabOrange lipgloss.Color = "#E05A3A"
	ColorWarmGold   lipgloss.Color = "#E8C785"
	ColorDeepOcean  lipgloss.Color = "#1A2B3D"
	ColorSeafoam    lipgloss.Color = "#5FBFAD"
	ColorCoralRed   lipgloss.Color = "#D94F4F"
	ColorDriftGray  lipgloss.Color = "#6B7B8D"
	ColorShellWhite lipgloss.Color = "#F0EDE6"
)

// Lipgloss style presets.
var (
	StyleHeading    = lipgloss.NewStyle().Bold(true).Foreground(ColorCrabOrange)
	StyleSubheading = lipgloss.NewStyle().Bold(true).Foreground(ColorWarmGold)
	StyleBody       = lipgloss.NewStyle().Foreground(ColorShellWhite)
	StyleMuted      = lipgloss.NewStyle().Foreground(ColorDriftGray)
	StyleSuccess    = lipgloss.NewStyle().Foreground(ColorSeafoam)
	StyleWarning    = lipgloss.NewStyle().Foreground(ColorCrabOrange)
	StyleError      = lipgloss.NewStyle().Foreground(ColorCoralRed)
	StyleSelected   = lipgloss.NewStyle().Bold(true).Foreground(ColorWarmGold)
	StyleBorder     = lipgloss.NewStyle().Foreground(ColorDriftGray)
)

// StyleDivider returns a repeating ─═ pattern rendered in DriftGray, truncated
// or padded to exactly width characters.
func StyleDivider(width int) string {
	if width <= 0 {
		return ""
	}
	pattern := "─═"
	var b strings.Builder
	for b.Len() < width {
		b.WriteString(pattern)
	}
	// Truncate to exact width (rune-safe since pattern is ASCII-width runes).
	runes := []rune(b.String())
	if len(runes) > width {
		runes = runes[:width]
	}
	return StyleMuted.Render(string(runes))
}

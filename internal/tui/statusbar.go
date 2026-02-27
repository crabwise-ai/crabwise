package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderStatusBar renders a bottom status bar with left-aligned key hints and
// right-aligned status text spanning the given width.
func RenderStatusBar(left string, right string, width int) string {
	leftRendered := StyleMuted.Render(left)
	rightRendered := StyleMuted.Render(right)

	// Calculate padding between left and right content.
	leftLen := lipgloss.Width(leftRendered)
	rightLen := lipgloss.Width(rightRendered)
	gap := width - leftLen - rightLen
	if gap < 1 {
		gap = 1
	}

	bar := leftRendered + strings.Repeat(" ", gap) + rightRendered

	return lipgloss.NewStyle().
		Width(width).
		Background(ColorDeepOcean).
		Render(bar)
}

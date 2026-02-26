package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const panelMaxWidth = 72

// RenderPanel renders a bordered panel with a title and body.
func RenderPanel(title, body string) string {
	// Determine content width from the widest line, capped at panelMaxWidth.
	width := lipgloss.Width(title)
	for _, line := range strings.Split(body, "\n") {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	// Add padding (2 chars each side).
	width += 4
	if width > panelMaxWidth {
		width = panelMaxWidth
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorDriftGray).
		Width(width).
		Padding(0, 1).
		Render(
			StyleHeading.Render(title) + "\n" +
				StyleBody.Render(body),
		)
}

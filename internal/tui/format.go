package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// FormatTimestamp renders a time as HH:MM:SS.
func FormatTimestamp(t time.Time) string {
	return t.Format("15:04:05")
}

// FormatDuration renders a duration in human-friendly form: 2h 14m, 45s, 3m 12s.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	d = d.Round(time.Second)

	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0 && s > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// Truncate truncates s to max characters, appending "..." if truncated.
func Truncate(s string, max int) string {
	if max <= 3 {
		if len(s) <= max {
			return s
		}
		return s[:max]
	}
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// StatusIcon maps a status string to a styled icon.
func StatusIcon(status string) string {
	switch strings.ToLower(status) {
	case "running", "active":
		return lipgloss.NewStyle().Foreground(ColorSeafoam).Render("◉")
	case "stopped", "inactive":
		return lipgloss.NewStyle().Foreground(ColorDriftGray).Render("○")
	case "warning", "warned":
		return lipgloss.NewStyle().Foreground(ColorCrabOrange).Render("⚠")
	case "blocked", "error":
		return lipgloss.NewStyle().Foreground(ColorCoralRed).Render("✖")
	case "success":
		return lipgloss.NewStyle().Foreground(ColorSeafoam).Render("✓")
	case "connecting":
		return lipgloss.NewStyle().Foreground(ColorWarmGold).Render("≋")
	default:
		return " "
	}
}

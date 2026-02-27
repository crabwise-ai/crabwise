package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

// Custom nautical spinner frame sets.
var (
	SpinnerLine = spinner.Line

	SpinnerTide = spinner.Spinner{
		Frames: []string{"░", "▒", "▓", "█", "▓", "▒", "░"},
		FPS:    80 * time.Millisecond,
	}

	SpinnerBubbles = spinner.Spinner{
		Frames: []string{"○", "◎", "◉", "●", "◉", "◎", "○"},
		FPS:    80 * time.Millisecond,
	}

	SpinnerDrift = spinner.Spinner{
		Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		FPS:    100 * time.Millisecond,
	}
)

// NewSpinner creates a pre-styled spinner model with CrabOrange foreground.
func NewSpinner(s spinner.Spinner) spinner.Model {
	sp := spinner.New()
	sp.Spinner = s
	sp.Style = lipgloss.NewStyle().Foreground(ColorCrabOrange)
	return sp
}

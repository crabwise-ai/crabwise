package tui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// TableOption configures a styled table.
type TableOption func(*tableOptions)

type tableOptions struct {
	height int
	width  int
}

// WithHeight sets the visible row count.
func WithHeight(h int) TableOption {
	return func(o *tableOptions) { o.height = h }
}

// WithWidth sets the table width.
func WithWidth(w int) TableOption {
	return func(o *tableOptions) { o.width = w }
}

// NewStyledTable creates a bubbles table.Model with the crabwise theme applied.
func NewStyledTable(columns []table.Column, rows []table.Row, opts ...TableOption) table.Model {
	o := tableOptions{height: 10}
	for _, opt := range opts {
		opt(&o)
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithHeight(o.height),
	)

	if o.width > 0 {
		t.SetWidth(o.width)
	}

	s := table.DefaultStyles()
	s.Header = s.Header.
		Bold(true).
		Foreground(ColorCrabOrange).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorDriftGray).
		BorderBottom(true)
	s.Selected = s.Selected.
		Foreground(ColorWarmGold).
		Bold(true)
	t.SetStyles(s)

	return t
}

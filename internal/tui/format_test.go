package tui

import (
	"testing"
	"time"
)

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"midnight", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), "00:00:00"},
		{"afternoon", time.Date(2025, 6, 15, 14, 30, 5, 0, time.UTC), "14:30:05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTimestamp(tt.in)
			if got != tt.want {
				t.Fatalf("FormatTimestamp(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"seconds", 45 * time.Second, "45s"},
		{"minutes_seconds", 3*time.Minute + 12*time.Second, "3m 12s"},
		{"minutes_only", 5 * time.Minute, "5m"},
		{"hours_minutes", 2*time.Hour + 14*time.Minute, "2h 14m"},
		{"hours_only", 3 * time.Hour, "3h"},
		{"hours_minutes_seconds", 1*time.Hour + 2*time.Minute + 30*time.Second, "1h 2m"},
		{"sub_second", 500 * time.Millisecond, "1s"},
		{"near_zero", 100 * time.Millisecond, "0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.in)
			if got != tt.want {
				t.Fatalf("FormatDuration(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"zero", 0, "$0"},
		{"small", 0.023, "$0.023"},
		{"medium", 1.45, "$1.45"},
		{"whole", 5.0, "$5"},
		{"trailing_zeros", 0.100, "$0.1"},
		{"three_decimals", 0.001, "$0.001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCost(tt.in)
			if got != tt.want {
				t.Fatalf("FormatCost(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world", 8, "hello..."},
		{"very_short_max", "hello", 2, "he"},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.s, tt.max)
			if got != tt.want {
				t.Fatalf("Truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status  string
		contain string
	}{
		{"running", "◉"},
		{"active", "◉"},
		{"stopped", "○"},
		{"inactive", "○"},
		{"warning", "⚠"},
		{"warned", "⚠"},
		{"blocked", "✖"},
		{"error", "✖"},
		{"success", "✓"},
		{"connecting", "≋"},
		{"unknown", " "},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := StatusIcon(tt.status)
			if got == "" {
				t.Fatalf("StatusIcon(%q) returned empty string", tt.status)
			}
			// The icon may be wrapped in ANSI codes; check it contains the rune.
			found := false
			for _, r := range got {
				for _, want := range tt.contain {
					if r == want {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("StatusIcon(%q) = %q, expected to contain %q", tt.status, got, tt.contain)
			}
		})
	}
}

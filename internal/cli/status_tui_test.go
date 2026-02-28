package cli

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crabwise-ai/crabwise/internal/daemon"
)

func TestStatusTUIModel_Update(t *testing.T) {
	cfg := &daemon.Config{}
	cfg.Daemon.SocketPath = "/tmp/nonexistent.sock"
	cfg.Adapters.Proxy.Listen = "127.0.0.1:9119"
	cfg.Adapters.LogWatcher.Enabled = true

	m := newStatusTUIModel(cfg)

	// Verify defaults
	if m.connected {
		t.Fatal("expected disconnected on init")
	}
	if m.width != 80 {
		t.Fatalf("expected default width 80, got %d", m.width)
	}

	// Send a statusPollMsg with sample data
	poll := statusPollMsg{
		connected:         true,
		uptime:            "2h 14m",
		pid:               float64(1234),
		agents:            float64(2),
		queueDepth:        12,
		queueDropped:      0,
		proxyReqs:         float64(847),
		proxyBlocked:      float64(3),
		proxyErrors:       float64(0),
		mappingDegraded:   float64(0),
		unclassifiedTools: float64(1),
		openclawConnected: true,
		openclawSessions:  float64(2),
		openclawMatches:   float64(5),
		openclawAmbiguous: float64(1),
	}

	updated, cmd := m.Update(poll)
	if cmd != nil {
		t.Fatalf("expected nil cmd from poll msg, got %T", cmd)
	}
	next := updated.(statusTUIModel)

	if !next.connected {
		t.Fatal("expected connected after poll")
	}
	if next.uptime != "2h 14m" {
		t.Fatalf("expected uptime '2h 14m', got %q", next.uptime)
	}
	if next.queueDepth != 12 {
		t.Fatalf("expected queue depth 12, got %d", next.queueDepth)
	}
	if next.queueDropped != 0 {
		t.Fatalf("expected queue dropped 0, got %d", next.queueDropped)
	}

	// View should contain key elements
	view := next.View()
	if !strings.Contains(view, "Status") {
		t.Fatalf("expected 'Status' heading in view, got: %s", view)
	}
	if !strings.Contains(view, "Daemon") {
		t.Fatalf("expected 'Daemon' in view, got: %s", view)
	}
	if !strings.Contains(view, "2h 14m") {
		t.Fatalf("expected uptime in view, got: %s", view)
	}
	if !strings.Contains(view, "Queue") {
		t.Fatalf("expected 'Queue' section in view, got: %s", view)
	}
	if !strings.Contains(view, "12") {
		t.Fatalf("expected queue depth 12 in view, got: %s", view)
	}
	if !strings.Contains(view, "OpenClaw") {
		t.Fatalf("expected OpenClaw in view, got: %s", view)
	}
	if !strings.Contains(view, "Matches:") {
		t.Fatalf("expected OpenClaw matches in view, got: %s", view)
	}

	// Test q key quits
	_, cmd = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit cmd from 'q' key")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

func TestStatusTUIModel_WindowResize(t *testing.T) {
	cfg := &daemon.Config{}
	cfg.Daemon.SocketPath = "/tmp/nonexistent.sock"

	m := newStatusTUIModel(cfg)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next := updated.(statusTUIModel)

	if next.width != 120 {
		t.Fatalf("expected width 120 after resize, got %d", next.width)
	}
}

func TestStatusTUIModel_DisconnectedView(t *testing.T) {
	cfg := &daemon.Config{}
	cfg.Daemon.SocketPath = "/tmp/nonexistent.sock"

	m := newStatusTUIModel(cfg)
	m.connected = false

	view := m.View()
	if !strings.Contains(view, "not running") {
		t.Fatalf("expected 'not running' in disconnected view, got: %s", view)
	}
	if !strings.Contains(view, "retry") {
		t.Fatalf("expected 'retry' hint in disconnected view, got: %s", view)
	}
}

func TestStatusTUIModel_ManualRefresh(t *testing.T) {
	cfg := &daemon.Config{}
	cfg.Daemon.SocketPath = "/tmp/nonexistent.sock"

	m := newStatusTUIModel(cfg)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("expected poll cmd from 'r' key")
	}
}

func TestStatusTUIModel_CtrlCQuits(t *testing.T) {
	cfg := &daemon.Config{}
	cfg.Daemon.SocketPath = "/tmp/nonexistent.sock"

	m := newStatusTUIModel(cfg)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd from ctrl+c")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", quitMsg)
	}
}

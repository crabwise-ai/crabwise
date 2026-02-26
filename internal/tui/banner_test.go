package tui

import (
	"strings"
	"testing"
)

func TestRenderBannerStatic_ContainsExpectedText(t *testing.T) {
	out := RenderBannerStatic("0.4.2")
	if out == "" {
		t.Fatal("RenderBannerStatic returned empty string")
	}

	checks := []string{
		"Crabwise AI v0.4.2",
		"Local-first AI agent governance",
		"github.com/crabwise-ai/crabwise",
		"▀██████████▀",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected static banner to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRenderBannerStatic_HasFourLines(t *testing.T) {
	out := RenderBannerStatic("1.0.0")
	lines := strings.Split(out, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d:\n%s", len(lines), out)
	}
}

func TestBannerModel_WaveProducesFrames(t *testing.T) {
	m := NewBannerModel("0.4.2")

	// Init should produce a command.
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil cmd")
	}

	// First frame should have content.
	view := m.View()
	if view == "" {
		t.Fatal("initial View() is empty")
	}

	// Advance one tick and verify model progresses.
	updated, cmd := m.Update(bannerTickMsg{})
	if cmd == nil {
		t.Fatal("expected tick cmd after first update")
	}
	m2 := updated.(BannerModel)
	if m2.pos != 1 {
		t.Fatalf("expected pos=1 after first tick, got %d", m2.pos)
	}

	// Advance until done.
	model := m2
	for !model.done {
		updated, _ = model.Update(bannerTickMsg{})
		model = updated.(BannerModel)
	}
	if !model.done {
		t.Fatal("expected model to be done after full sweep")
	}

	// Final view should still have content.
	finalView := model.View()
	if finalView == "" {
		t.Fatal("final View() is empty")
	}
}

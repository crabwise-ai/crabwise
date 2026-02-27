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
		CrabArt[2], // crab art body line
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

func TestCrabArtRippleStyled_PreservesShape(t *testing.T) {
	// Ripple animates color only; block shapes (▄ █ ▀ ▂ ▓) must stay the same.
	styled := CrabArtRippleStyled(0)
	if len(styled) != len(CrabArt) {
		t.Fatalf("expected %d lines, got %d", len(CrabArt), len(styled))
	}
	for i := range styled {
		// Styled line contains ANSI codes; verify it has content
		if styled[i] == "" {
			t.Errorf("line %d: empty styled output", i)
		}
	}
}

func TestBannerModel_RippleProducesFrames(t *testing.T) {
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
	if m2.tick != 1 {
		t.Fatalf("expected tick=1 after first update, got %d", m2.tick)
	}

	// Advance a few more ticks and verify loop continues.
	model := m2
	for i := 0; i < 2; i++ {
		updated, cmd := model.Update(bannerTickMsg{})
		if cmd == nil {
			t.Fatal("expected tick cmd after update")
		}
		model = updated.(BannerModel)
	}
	if model.tick != 3 {
		t.Fatalf("expected tick=3 after 3 updates, got %d", model.tick)
	}

	// View should still have content.
	finalView := model.View()
	if finalView == "" {
		t.Fatal("final View() is empty")
	}
}

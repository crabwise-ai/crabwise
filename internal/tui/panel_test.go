package tui

import (
	"strings"
	"testing"
)

func TestRenderPanel_NonEmpty(t *testing.T) {
	out := RenderPanel("Test Title", "Some body text here")
	if out == "" {
		t.Fatal("RenderPanel returned empty string")
	}
	if !strings.Contains(out, "Test Title") {
		t.Fatalf("expected panel to contain title, got: %s", out)
	}
	if !strings.Contains(out, "Some body text here") {
		t.Fatalf("expected panel to contain body, got: %s", out)
	}
}

func TestRenderPanel_MultilineBody(t *testing.T) {
	body := "line one\nline two\nline three"
	out := RenderPanel("Multi", body)
	if !strings.Contains(out, "line one") {
		t.Fatalf("expected multiline body content, got: %s", out)
	}
	if !strings.Contains(out, "line three") {
		t.Fatalf("expected multiline body content, got: %s", out)
	}
}

func TestRenderStatusBar_NonEmpty(t *testing.T) {
	out := RenderStatusBar("q: quit | /: filter", "uptime: 5m", 80)
	if out == "" {
		t.Fatal("RenderStatusBar returned empty string")
	}
	if !strings.Contains(out, "quit") {
		t.Fatalf("expected status bar to contain left text, got: %s", out)
	}
	if !strings.Contains(out, "uptime") {
		t.Fatalf("expected status bar to contain right text, got: %s", out)
	}
}

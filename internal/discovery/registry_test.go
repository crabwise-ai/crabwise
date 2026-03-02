package discovery

import (
	"testing"
	"time"
)

func TestRegistryReplaceSource_PreservesOtherSources(t *testing.T) {
	registry := NewRegistry()

	registry.ReplaceSource("openclaw-gateway", []AgentInfo{
		{
			ID:             "openclaw/agent:main:discord:channel:123",
			Type:           "openclaw",
			LastActivityAt: time.Now(),
		},
	})
	registry.ReplaceSource("scanner", []AgentInfo{
		{
			ID:     "codex/pid-123",
			Type:   "codex",
			PID:    123,
			Status: "active",
		},
	})
	registry.ReplaceSource("scanner", nil)

	if _, ok := registry.Get("openclaw/agent:main:discord:channel:123"); !ok {
		t.Fatal("expected openclaw agent to remain after scanner refresh")
	}
	if _, ok := registry.Get("codex/pid-123"); ok {
		t.Fatal("expected scanner agent to be removed when scanner source is replaced with empty set")
	}
}

func TestRegistryReplaceSource_OpenClawStatusUsesRecency(t *testing.T) {
	registry := NewRegistry()

	registry.ReplaceSource("openclaw-gateway", []AgentInfo{
		{
			ID:             "openclaw/agent:main:discord:channel:123",
			Type:           "openclaw",
			LastActivityAt: time.Now().Add(-2 * time.Minute),
		},
		{
			ID:             "openclaw/agent:main:discord:channel:456",
			Type:           "openclaw",
			LastActivityAt: time.Now().Add(-10 * time.Minute),
		},
	})

	active, ok := registry.Get("openclaw/agent:main:discord:channel:123")
	if !ok {
		t.Fatal("expected active openclaw agent")
	}
	if active.Status != "active" {
		t.Fatalf("expected active status, got %q", active.Status)
	}
	if active.PID != 0 {
		t.Fatalf("expected pid-less openclaw agent, got pid %d", active.PID)
	}

	inactive, ok := registry.Get("openclaw/agent:main:discord:channel:456")
	if !ok {
		t.Fatal("expected inactive openclaw agent")
	}
	if inactive.Status != "inactive" {
		t.Fatalf("expected inactive status, got %q", inactive.Status)
	}
}

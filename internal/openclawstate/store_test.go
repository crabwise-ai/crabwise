package openclawstate

import (
	"testing"
	"time"
)

func TestRecordSessionAndRun(t *testing.T) {
	store := New(3 * time.Second)

	store.RecordSession(SessionMeta{
		SessionKey:    "agent:main:discord:channel:123",
		AgentID:       "main",
		Model:         "claude-sonnet",
		ThinkingLevel: "high",
	})
	store.RecordRun("run-1", "agent:main:discord:channel:123")

	snapshot := store.Snapshot()
	sessions, ok := snapshot["sessions"].(map[string]SessionMeta)
	if !ok {
		t.Fatalf("expected sessions snapshot, got %#v", snapshot["sessions"])
	}
	meta, ok := sessions["agent:main:discord:channel:123"]
	if !ok {
		t.Fatal("expected session metadata in snapshot")
	}
	if meta.AgentID != "main" {
		t.Fatalf("expected agent id main, got %q", meta.AgentID)
	}

	runs, ok := snapshot["runs"].(map[string]string)
	if !ok {
		t.Fatalf("expected runs snapshot, got %#v", snapshot["runs"])
	}
	if runs["run-1"] != "agent:main:discord:channel:123" {
		t.Fatalf("expected run mapping, got %#v", runs)
	}
}

func TestMatchRequest(t *testing.T) {
	now := time.Now()
	store := New(3 * time.Second)
	store.RecordSession(SessionMeta{
		SessionKey:    "agent:main:discord:channel:123",
		AgentID:       "main",
		Model:         "claude-sonnet",
		ThinkingLevel: "high",
	})
	store.RecordChat("run-1", "agent:main:discord:channel:123", "anthropic", "claude-sonnet", now.Add(-500*time.Millisecond))

	got, ok := store.MatchProxyRequest(now, "anthropic", "claude-sonnet")
	if !ok {
		t.Fatal("expected match")
	}
	if got.AgentID != "openclaw" {
		t.Fatalf("expected agent id openclaw, got %q", got.AgentID)
	}
	if got.SessionKey != "agent:main:discord:channel:123" {
		t.Fatalf("expected session key, got %q", got.SessionKey)
	}
	if got.Provider != "anthropic" {
		t.Fatalf("expected provider anthropic, got %q", got.Provider)
	}
	if got.Model != "claude-sonnet" {
		t.Fatalf("expected model claude-sonnet, got %q", got.Model)
	}
}

func TestMatchRequest_AmbiguousReturnsFalse(t *testing.T) {
	now := time.Now()
	store := New(3 * time.Second)
	store.RecordSession(SessionMeta{SessionKey: "agent:main:discord:channel:1", AgentID: "main", Model: "claude-sonnet"})
	store.RecordSession(SessionMeta{SessionKey: "agent:main:discord:channel:2", AgentID: "main", Model: "claude-sonnet"})
	store.RecordChat("run-1", "agent:main:discord:channel:1", "anthropic", "claude-sonnet", now.Add(-time.Second))
	store.RecordChat("run-2", "agent:main:discord:channel:2", "anthropic", "claude-sonnet", now.Add(-time.Second))

	_, ok := store.MatchProxyRequest(now, "anthropic", "claude-sonnet")
	if ok {
		t.Fatal("expected ambiguous match to return false")
	}
}

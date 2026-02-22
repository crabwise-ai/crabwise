package audit

import (
	"testing"
	"time"
)

func TestCanonicalBytes_Deterministic(t *testing.T) {
	ts := time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC)
	e := &AuditEvent{
		ID:         "evt_001",
		Timestamp:  ts,
		AgentID:    "claude-code",
		ActionType: ActionToolCall,
		Action:     "Read",
		Outcome:    OutcomeSuccess,
		SessionID:  "sess_abc",
	}

	b1 := CanonicalBytes(e)
	b2 := CanonicalBytes(e)

	if len(b1) == 0 {
		t.Fatal("canonical bytes should not be empty")
	}
	if string(b1) != string(b2) {
		t.Fatal("canonical bytes not deterministic")
	}
}

func TestComputeHash_Genesis(t *testing.T) {
	ts := time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC)
	e := &AuditEvent{
		ID:         "evt_001",
		Timestamp:  ts,
		AgentID:    "claude-code",
		ActionType: ActionToolCall,
		Action:     "Read",
		Outcome:    OutcomeSuccess,
	}

	hash := ComputeHash(e, "genesis")
	if len(hash) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d chars", len(hash))
	}

	// Same inputs should produce same hash
	hash2 := ComputeHash(e, "genesis")
	if hash != hash2 {
		t.Fatal("hash not deterministic")
	}
}

func TestComputeHash_ChainDiffers(t *testing.T) {
	ts := time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC)
	e := &AuditEvent{
		ID:         "evt_001",
		Timestamp:  ts,
		AgentID:    "claude-code",
		ActionType: ActionToolCall,
		Outcome:    OutcomeSuccess,
	}

	h1 := ComputeHash(e, "genesis")
	h2 := ComputeHash(e, "someprevhash")

	if h1 == h2 {
		t.Fatal("different prev_hash should produce different event_hash")
	}
}

func TestComputeHash_FieldChange(t *testing.T) {
	ts := time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC)
	e1 := &AuditEvent{
		ID:         "evt_001",
		Timestamp:  ts,
		AgentID:    "claude-code",
		ActionType: ActionToolCall,
		Outcome:    OutcomeSuccess,
	}
	e2 := &AuditEvent{
		ID:         "evt_001",
		Timestamp:  ts,
		AgentID:    "claude-code",
		ActionType: ActionToolCall,
		Outcome:    OutcomeFailure, // different
	}

	h1 := ComputeHash(e1, "genesis")
	h2 := ComputeHash(e2, "genesis")

	if h1 == h2 {
		t.Fatal("different outcome should produce different hash")
	}
}

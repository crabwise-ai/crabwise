package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func tempDB(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s, func() {
		s.Close()
		os.RemoveAll(dir)
	}
}

func TestOpenAndMigrate(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	var version int
	err := s.DB().QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected version 1, got %d", version)
	}
}

func TestInsertAndQueryEvents(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	events := []*audit.AuditEvent{
		{
			ID:         "evt_001",
			Timestamp:  time.Now().UTC(),
			AgentID:    "claude-code",
			ActionType: audit.ActionToolCall,
			Action:     "Read",
			Outcome:    audit.OutcomeSuccess,
			EventHash:  "abc123",
			PrevHash:   "genesis",
		},
		{
			ID:         "evt_002",
			Timestamp:  time.Now().UTC(),
			AgentID:    "claude-code",
			ActionType: audit.ActionCommandExecution,
			Action:     "Bash",
			Outcome:    audit.OutcomeSuccess,
			EventHash:  "def456",
			PrevHash:   "abc123",
		},
	}

	if err := s.InsertEvents(events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	var count int
	s.DB().QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 events, got %d", count)
	}
}

func TestInsertEvents_DuplicateIgnored(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	e := &audit.AuditEvent{
		ID:         "evt_dup",
		Timestamp:  time.Now().UTC(),
		AgentID:    "claude-code",
		ActionType: audit.ActionToolCall,
		Outcome:    audit.OutcomeSuccess,
		EventHash:  "hash1",
		PrevHash:   "genesis",
	}

	if err := s.InsertEvents([]*audit.AuditEvent{e}); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := s.InsertEvents([]*audit.AuditEvent{e}); err != nil {
		t.Fatalf("duplicate insert should not error: %v", err)
	}

	var count int
	s.DB().QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 event after duplicate, got %d", count)
	}
}

func TestFileOffsets(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	offset, err := s.GetFileOffset("/tmp/test.jsonl")
	if err != nil {
		t.Fatalf("get offset: %v", err)
	}
	if offset != 0 {
		t.Fatalf("expected 0 for unknown file, got %d", offset)
	}

	if err := s.SetFileOffset("/tmp/test.jsonl", 1024); err != nil {
		t.Fatalf("set offset: %v", err)
	}

	offset, err = s.GetFileOffset("/tmp/test.jsonl")
	if err != nil {
		t.Fatalf("get offset after set: %v", err)
	}
	if offset != 1024 {
		t.Fatalf("expected 1024, got %d", offset)
	}

	// Update
	if err := s.SetFileOffset("/tmp/test.jsonl", 2048); err != nil {
		t.Fatalf("update offset: %v", err)
	}
	offset, _ = s.GetFileOffset("/tmp/test.jsonl")
	if offset != 2048 {
		t.Fatalf("expected 2048, got %d", offset)
	}
}

func TestInsertEvents_AtomicOffsets(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	events := []*audit.AuditEvent{
		{
			ID: "evt_a1", Timestamp: time.Now().UTC(),
			AgentID: "claude-code", ActionType: audit.ActionToolCall, Outcome: audit.OutcomeSuccess,
			EventHash: "h1", PrevHash: "genesis",
			SourceFile: "/tmp/session1.jsonl", SourceOffset: 500,
		},
		{
			ID: "evt_a2", Timestamp: time.Now().UTC(),
			AgentID: "claude-code", ActionType: audit.ActionToolCall, Outcome: audit.OutcomeSuccess,
			EventHash: "h2", PrevHash: "h1",
			SourceFile: "/tmp/session1.jsonl", SourceOffset: 1000,
		},
		{
			ID: "evt_b1", Timestamp: time.Now().UTC(),
			AgentID: "claude-code", ActionType: audit.ActionToolCall, Outcome: audit.OutcomeSuccess,
			EventHash: "h3", PrevHash: "h2",
			SourceFile: "/tmp/session2.jsonl", SourceOffset: 300,
		},
	}

	if err := s.InsertEvents(events); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Verify offsets were committed atomically — highest offset per file
	offset1, _ := s.GetFileOffset("/tmp/session1.jsonl")
	if offset1 != 1000 {
		t.Fatalf("expected offset 1000 for session1, got %d", offset1)
	}
	offset2, _ := s.GetFileOffset("/tmp/session2.jsonl")
	if offset2 != 300 {
		t.Fatalf("expected offset 300 for session2, got %d", offset2)
	}

	// Events without SourceFile should not affect offsets
	events2 := []*audit.AuditEvent{
		{
			ID: "evt_c1", Timestamp: time.Now().UTC(),
			AgentID: "crabwise", ActionType: audit.ActionSystem, Outcome: audit.OutcomeSuccess,
			EventHash: "h4", PrevHash: "h3",
			// No SourceFile
		},
	}
	if err := s.InsertEvents(events2); err != nil {
		t.Fatalf("insert no-source: %v", err)
	}
	// Offsets should be unchanged
	offset1After, _ := s.GetFileOffset("/tmp/session1.jsonl")
	if offset1After != 1000 {
		t.Fatalf("offset should be unchanged, got %d", offset1After)
	}
}

func TestGetLastEventHash_Empty(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	hash, err := s.GetLastEventHash()
	if err != nil {
		t.Fatalf("get last hash: %v", err)
	}
	if hash != "genesis" {
		t.Fatalf("expected 'genesis' for empty db, got %q", hash)
	}
}

func TestGetLastEventHash_WithEvents(t *testing.T) {
	s, cleanup := tempDB(t)
	defer cleanup()

	events := []*audit.AuditEvent{
		{
			ID:        "evt_001",
			Timestamp: time.Now().UTC().Add(-time.Minute),
			AgentID:   "a", ActionType: audit.ActionToolCall, Outcome: audit.OutcomeSuccess,
			EventHash: "hash_first", PrevHash: "genesis",
		},
		{
			ID:        "evt_002",
			Timestamp: time.Now().UTC(),
			AgentID:   "a", ActionType: audit.ActionToolCall, Outcome: audit.OutcomeSuccess,
			EventHash: "hash_second", PrevHash: "hash_first",
		},
	}
	s.InsertEvents(events)

	hash, _ := s.GetLastEventHash()
	if hash != "hash_second" {
		t.Fatalf("expected hash_second, got %q", hash)
	}
}

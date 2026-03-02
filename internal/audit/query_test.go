package audit

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupQueryTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "query.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	create := `CREATE TABLE events (
		id TEXT PRIMARY KEY,
		timestamp TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		agent_pid INTEGER,
		action_type TEXT NOT NULL,
		action TEXT,
		arguments TEXT,
		session_id TEXT,
		parent_session_id TEXT,
		working_dir TEXT,
		parser_version TEXT,
		outcome TEXT NOT NULL,
		commandments_evaluated TEXT,
		commandments_triggered TEXT,
		provider TEXT,
		model TEXT,
		tool_category TEXT,
		tool_effect TEXT,
		tool_name TEXT,
		taxonomy_version TEXT,
		classification_source TEXT,
		input_tokens INTEGER,
		output_tokens INTEGER,
		adapter_id TEXT,
		adapter_type TEXT,
		raw_payload_ref TEXT,
		prev_hash TEXT,
		event_hash TEXT NOT NULL,
		redacted INTEGER DEFAULT 0,
		hostname TEXT,
		user_id TEXT
	);`
	if _, err := db.Exec(create); err != nil {
		t.Fatalf("create table: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func insertQueryEvent(t *testing.T, db *sql.DB, id, outcome, triggered string) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO events (id, timestamp, agent_id, action_type, outcome, commandments_triggered, event_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id,
		time.Now().UTC().Format(time.RFC3339Nano),
		"claude-code",
		"tool_call",
		outcome,
		triggered,
		"hash_"+id,
	)
	if err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
}

func TestQueryEvents_TriggeredOnly(t *testing.T) {
	db := setupQueryTestDB(t)
	insertQueryEvent(t, db, "evt_none", "success", "[]")
	insertQueryEvent(t, db, "evt_trig", "warned", `[{"name":"r1","enforcement":"warn"}]`)
	insertQueryEvent(t, db, "evt_empty", "success", "")

	result, err := QueryEvents(db, QueryFilter{TriggeredOnly: true})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if result.Total != 1 {
		t.Fatalf("expected total 1, got %d", result.Total)
	}
	if len(result.Events) != 1 || result.Events[0].ID != "evt_trig" {
		t.Fatalf("expected only evt_trig, got %+v", result.Events)
	}
}

func TestQueryEvents_TriggeredAndOutcome(t *testing.T) {
	db := setupQueryTestDB(t)
	insertQueryEvent(t, db, "evt_warned", "warned", `[{"name":"r1","enforcement":"warn"}]`)
	insertQueryEvent(t, db, "evt_blocked", "blocked", `[{"name":"r2","enforcement":"block"}]`)

	result, err := QueryEvents(db, QueryFilter{TriggeredOnly: true, Outcome: "warned"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if result.Total != 1 || len(result.Events) != 1 {
		t.Fatalf("expected one warned triggered event, got total=%d len=%d", result.Total, len(result.Events))
	}
	if result.Events[0].ID != "evt_warned" {
		t.Fatalf("expected evt_warned, got %s", result.Events[0].ID)
	}
}

func TestQueryEvents_ReturnsToolTaxonomyFields(t *testing.T) {
	db := setupQueryTestDB(t)
	_, err := db.Exec(`INSERT INTO events (
		id, timestamp, agent_id, action_type, outcome, event_hash,
		tool_category, tool_effect, tool_name, taxonomy_version, classification_source
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"evt_taxonomy",
		time.Now().UTC().Format(time.RFC3339Nano),
		"claude-code",
		"file_access",
		"success",
		"hash_evt_taxonomy",
		"file.read",
		"read_only",
		"Read",
		"v1",
		"exact",
	)
	if err != nil {
		t.Fatalf("insert taxonomy event: %v", err)
	}

	result, err := QueryEvents(db, QueryFilter{Limit: 1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Events))
	}
	if result.Events[0].ToolCategory != "file.read" || result.Events[0].ClassificationSource != "exact" {
		t.Fatalf("expected taxonomy fields to be scanned, got %+v", result.Events[0])
	}
}

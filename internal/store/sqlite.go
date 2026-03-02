package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var migration001 string

type Store struct {
	db *sql.DB
}

func Open(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate() error {
	var version int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		// Table doesn't exist yet — start from 0
		version = 0
	}
	if version < 1 {
		if _, err = s.db.Exec(migration001); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) InsertEvents(events []*audit.AuditEvent) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO events (
		id, timestamp, agent_id, agent_pid, action_type, action, arguments,
		session_id, parent_session_id, working_dir, parser_version, outcome,
		commandments_evaluated, commandments_triggered,
		provider, model, tool_category, tool_effect, tool_name, taxonomy_version, classification_source,
		input_tokens, output_tokens,
		adapter_id, adapter_type, raw_payload_ref, prev_hash, event_hash, redacted,
		hostname, user_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	// Collect file offsets to update atomically with event inserts
	offsets := make(map[string]int64)

	for _, e := range events {
		redacted := 0
		if e.Redacted {
			redacted = 1
		}
		_, err := stmt.Exec(
			e.ID, e.Timestamp.UTC().Format(time.RFC3339Nano),
			e.AgentID, e.AgentPID, string(e.ActionType), e.Action, e.Arguments,
			e.SessionID, e.ParentSessionID, e.WorkingDir, e.ParserVersion, string(e.Outcome),
			e.CommandmentsEvaluated, e.CommandmentsTriggered,
			e.Provider, e.Model, e.ToolCategory, e.ToolEffect, e.ToolName, e.TaxonomyVersion, e.ClassificationSource,
			e.InputTokens, e.OutputTokens,
			e.AdapterID, e.AdapterType, e.RawPayloadRef, e.PrevHash, e.EventHash, redacted,
			e.Hostname, e.UserID,
		)
		if err != nil {
			return fmt.Errorf("insert event %s: %w", e.ID, err)
		}

		// Track highest offset per source file
		if e.SourceFile != "" && e.SourceOffset > offsets[e.SourceFile] {
			offsets[e.SourceFile] = e.SourceOffset
		}
	}

	// Commit file offsets in same transaction — atomic with event inserts
	for path, offset := range offsets {
		_, err := tx.Exec(
			`INSERT INTO file_offsets (file_path, offset, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(file_path) DO UPDATE SET offset = excluded.offset, updated_at = excluded.updated_at`,
			path, offset, time.Now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("update offset %s: %w", path, err)
		}
	}

	return tx.Commit()
}

func (s *Store) GetFileOffset(filePath string) (int64, error) {
	var offset int64
	err := s.db.QueryRow("SELECT offset FROM file_offsets WHERE file_path = ?", filePath).Scan(&offset)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return offset, err
}

func (s *Store) SetFileOffset(filePath string, offset int64) error {
	_, err := s.db.Exec(
		`INSERT INTO file_offsets (file_path, offset, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(file_path) DO UPDATE SET offset = excluded.offset, updated_at = excluded.updated_at`,
		filePath, offset, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) InsertChainAnchor(epoch, eventID, eventHash string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO chain_anchors (epoch, event_id, event_hash, created_at) VALUES (?, ?, ?, ?)`,
		epoch, eventID, eventHash, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) GetLastEventHash() (string, error) {
	var hash string
	err := s.db.QueryRow("SELECT event_hash FROM events ORDER BY timestamp DESC, rowid DESC LIMIT 1").Scan(&hash)
	if err == sql.ErrNoRows {
		return "genesis", nil
	}
	return hash, err
}

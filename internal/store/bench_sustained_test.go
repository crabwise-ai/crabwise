//go:build m3_bench

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestSQLiteBatchInsertThroughput(t *testing.T) {
	t.Log("m3_bench_profile sqlite_throughput")

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	const (
		totalEvents = 10000
		batchSize   = 100
	)

	// Generate events
	events := make([]*audit.AuditEvent, totalEvents)
	now := time.Now().UTC()
	for i := 0; i < totalEvents; i++ {
		events[i] = &audit.AuditEvent{
			ID:          fmt.Sprintf("bench-%06d", i),
			Timestamp:   now.Add(time.Duration(i) * time.Millisecond),
			AgentID:     "bench-agent",
			ActionType:  audit.ActionToolCall,
			Action:      "Bash",
			Arguments:   fmt.Sprintf(`{"command":"echo %d","cwd":"/tmp"}`, i),
			Outcome:     audit.OutcomeSuccess,
			Provider:    "openai",
			Model:       "gpt-4o",
			AdapterID:   "proxy",
			AdapterType: "proxy",
			PrevHash:    "genesis",
			EventHash:   fmt.Sprintf("hash-%06d", i),
		}
	}

	start := time.Now()

	for i := 0; i < totalEvents; i += batchSize {
		end := i + batchSize
		if end > totalEvents {
			end = totalEvents
		}
		if err := s.InsertEvents(events[i:end]); err != nil {
			t.Fatalf("batch insert at %d: %v", i, err)
		}
	}

	elapsed := time.Since(start)
	throughput := float64(totalEvents) / elapsed.Seconds()

	t.Logf("m3_bench sqlite_throughput events=%d batches=%d batch_size=%d elapsed=%s throughput=%.0f events/sec",
		totalEvents, totalEvents/batchSize, batchSize, elapsed, throughput)

	// Verify DB exists
	fi, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	t.Logf("m3_bench sqlite_throughput db_size=%dKB", fi.Size()/1024)

	// Gate: throughput must be > 1000 events/sec
	if throughput < 1000 {
		t.Fatalf("SQLite batch insert throughput too low: %.0f events/sec (minimum 1000)", throughput)
	}
}

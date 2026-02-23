package audit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/queue"
)

type loggerTestStore struct {
	mu      sync.Mutex
	events  []*AuditEvent
	insertC chan []*AuditEvent
}

func newLoggerTestStore() *loggerTestStore {
	return &loggerTestStore{insertC: make(chan []*AuditEvent, 8)}
}

func (s *loggerTestStore) InsertEvents(events []*AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	batch := make([]*AuditEvent, 0, len(events))
	for _, e := range events {
		copied := *e
		batch = append(batch, &copied)
		s.events = append(s.events, &copied)
	}

	s.insertC <- batch
	return nil
}

func (s *loggerTestStore) GetLastEventHash() (string, error) {
	return "genesis", nil
}

func (s *loggerTestStore) InsertChainAnchor(epoch, eventID, eventHash string) error {
	return nil
}

func waitInsertedBatch(t *testing.T, s *loggerTestStore) []*AuditEvent {
	t.Helper()
	select {
	case batch := <-s.insertC:
		return batch
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for logger flush")
		return nil
	}
}

type evaluatorFunc func(*AuditEvent) EvalResult

func (f evaluatorFunc) Evaluate(e *AuditEvent) EvalResult {
	return f(e)
}

type redactorFunc func(*AuditEvent, bool)

func (f redactorFunc) Redact(e *AuditEvent, ruleTriggered bool) {
	f(e, ruleTriggered)
}

func TestLoggerProcessEvent_CommandmentsAndDowngrade(t *testing.T) {
	store := newLoggerTestStore()
	q := queue.New(16, queue.PolicyBlockWithTimeout, 10*time.Millisecond)

	logger, err := NewLogger(store, q, 1, time.Hour)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	var redactorCalled bool
	var redactorTriggered bool
	logger.SetEvaluator(evaluatorFunc(func(e *AuditEvent) EvalResult {
		return EvalResult{
			Evaluated: []string{"rule-b", "rule-a"},
			Triggered: []TriggeredRule{
				{Name: "rule-block", Enforcement: "block", Message: "danger"},
				{Name: "rule-warn", Enforcement: "warn", Message: "careful"},
			},
		}
	}))
	logger.SetRedactor(redactorFunc(func(e *AuditEvent, ruleTriggered bool) {
		redactorCalled = true
		redactorTriggered = ruleTriggered
		e.Arguments = "[REDACTED]"
		e.Redacted = true
	}))

	ctx, cancel := context.WithCancel(context.Background())
	logger.Start(ctx)
	defer func() {
		cancel()
		logger.Stop()
	}()

	q.Send(&AuditEvent{
		ID:          "evt_1",
		Timestamp:   time.Now().UTC(),
		AgentID:     "claude-code",
		ActionType:  ActionCommandExecution,
		Action:      "Bash",
		Arguments:   "rm -rf /tmp/demo",
		AdapterType: "log_watcher",
		Outcome:     OutcomeSuccess,
	})

	batch := waitInsertedBatch(t, store)
	if len(batch) != 1 {
		t.Fatalf("expected 1 event, got %d", len(batch))
	}

	e := batch[0]
	if !redactorCalled || !redactorTriggered {
		t.Fatalf("expected redactor to be called with ruleTriggered=true")
	}
	if e.Outcome != OutcomeWarned {
		t.Fatalf("expected warned outcome after block downgrade, got %s", e.Outcome)
	}
	if e.CommandmentsEvaluated != `["rule-b","rule-a"]` {
		t.Fatalf("unexpected commandments_evaluated: %s", e.CommandmentsEvaluated)
	}
	expectedTriggered := `[{"name":"rule-block","enforcement":"block","message":"danger"},{"name":"rule-warn","enforcement":"warn","message":"careful"}]`
	if e.CommandmentsTriggered != expectedTriggered {
		t.Fatalf("unexpected commandments_triggered: %s", e.CommandmentsTriggered)
	}
	if e.Arguments != "[REDACTED]" || !e.Redacted {
		t.Fatalf("expected redacted arguments")
	}
}

func TestLoggerProcessEvent_OutcomePrecedenceOnlyUpgrades(t *testing.T) {
	store := newLoggerTestStore()
	q := queue.New(16, queue.PolicyBlockWithTimeout, 10*time.Millisecond)

	logger, err := NewLogger(store, q, 1, time.Hour)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.SetEvaluator(evaluatorFunc(func(e *AuditEvent) EvalResult {
		switch e.ID {
		case "evt_failure":
			return EvalResult{Evaluated: []string{"warn-only"}, Triggered: []TriggeredRule{{Name: "warn-only", Enforcement: "warn"}}}
		case "evt_block":
			return EvalResult{Evaluated: []string{"block-rule"}, Triggered: []TriggeredRule{{Name: "block-rule", Enforcement: "block"}}}
		default:
			return EvalResult{Evaluated: []string{}, Triggered: []TriggeredRule{}}
		}
	}))

	ctx, cancel := context.WithCancel(context.Background())
	logger.Start(ctx)
	defer func() {
		cancel()
		logger.Stop()
	}()

	q.Send(&AuditEvent{
		ID:         "evt_failure",
		Timestamp:  time.Now().UTC(),
		AgentID:    "claude-code",
		ActionType: ActionToolCall,
		Outcome:    OutcomeFailure,
	})
	q.Send(&AuditEvent{
		ID:          "evt_block",
		Timestamp:   time.Now().UTC(),
		AgentID:     "claude-code",
		ActionType:  ActionCommandExecution,
		AdapterType: "daemon",
		Outcome:     OutcomeWarned,
	})

	first := waitInsertedBatch(t, store)
	second := waitInsertedBatch(t, store)

	if first[0].ID == "evt_failure" {
		if first[0].Outcome != OutcomeFailure {
			t.Fatalf("warn trigger must not downgrade failure, got %s", first[0].Outcome)
		}
		if second[0].Outcome != OutcomeBlocked {
			t.Fatalf("block trigger should upgrade warned to blocked, got %s", second[0].Outcome)
		}
		return
	}

	if first[0].Outcome != OutcomeBlocked {
		t.Fatalf("block trigger should upgrade warned to blocked, got %s", first[0].Outcome)
	}
	if second[0].Outcome != OutcomeFailure {
		t.Fatalf("warn trigger must not downgrade failure, got %s", second[0].Outcome)
	}
}

func TestOutcomePrecedenceMatrix(t *testing.T) {
	tests := []struct {
		name        string
		start       Outcome
		enforcement string
		adapterType string
		expected    Outcome
	}{
		// daemon + warn
		{name: "daemon_warn_from_success", start: OutcomeSuccess, enforcement: "warn", adapterType: "daemon", expected: OutcomeWarned},
		{name: "daemon_warn_from_warned", start: OutcomeWarned, enforcement: "warn", adapterType: "daemon", expected: OutcomeWarned},
		{name: "daemon_warn_from_failure", start: OutcomeFailure, enforcement: "warn", adapterType: "daemon", expected: OutcomeFailure},
		{name: "daemon_warn_from_blocked", start: OutcomeBlocked, enforcement: "warn", adapterType: "daemon", expected: OutcomeBlocked},

		// daemon + block
		{name: "daemon_block_from_success", start: OutcomeSuccess, enforcement: "block", adapterType: "daemon", expected: OutcomeBlocked},
		{name: "daemon_block_from_warned", start: OutcomeWarned, enforcement: "block", adapterType: "daemon", expected: OutcomeBlocked},
		{name: "daemon_block_from_failure", start: OutcomeFailure, enforcement: "block", adapterType: "daemon", expected: OutcomeBlocked},
		{name: "daemon_block_from_blocked", start: OutcomeBlocked, enforcement: "block", adapterType: "daemon", expected: OutcomeBlocked},

		// log_watcher + warn
		{name: "logwatcher_warn_from_success", start: OutcomeSuccess, enforcement: "warn", adapterType: "log_watcher", expected: OutcomeWarned},
		{name: "logwatcher_warn_from_warned", start: OutcomeWarned, enforcement: "warn", adapterType: "log_watcher", expected: OutcomeWarned},
		{name: "logwatcher_warn_from_failure", start: OutcomeFailure, enforcement: "warn", adapterType: "log_watcher", expected: OutcomeFailure},
		{name: "logwatcher_warn_from_blocked", start: OutcomeBlocked, enforcement: "warn", adapterType: "log_watcher", expected: OutcomeBlocked},

		// log_watcher + block (downgraded to warn)
		{name: "logwatcher_block_from_success", start: OutcomeSuccess, enforcement: "block", adapterType: "log_watcher", expected: OutcomeWarned},
		{name: "logwatcher_block_from_warned", start: OutcomeWarned, enforcement: "block", adapterType: "log_watcher", expected: OutcomeWarned},
		{name: "logwatcher_block_from_failure", start: OutcomeFailure, enforcement: "block", adapterType: "log_watcher", expected: OutcomeFailure},
		{name: "logwatcher_block_from_blocked", start: OutcomeBlocked, enforcement: "block", adapterType: "log_watcher", expected: OutcomeBlocked},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := outcomeForEnforcement(tc.enforcement, tc.adapterType)
			got := tc.start
			if outcomeRank(candidate) > outcomeRank(got) {
				got = candidate
			}

			if got != tc.expected {
				t.Fatalf("start=%s enforcement=%s adapter=%s: got=%s want=%s", tc.start, tc.enforcement, tc.adapterType, got, tc.expected)
			}
		})
	}

	if len(tests) != 16 {
		t.Fatalf("expected exhaustive 16-case matrix, got %d", len(tests))
	}
}

func TestLoggerProcessEvent_ExemptSystemEventsSkipEvaluationAndRedaction(t *testing.T) {
	tests := []string{
		"commandments_reload_ok",
		"commandments_reload_failed",
		"commandments_load_failed",
		"commandments_load_ok",
	}

	for _, action := range tests {
		t.Run(action, func(t *testing.T) {
			store := newLoggerTestStore()
			q := queue.New(16, queue.PolicyBlockWithTimeout, 10*time.Millisecond)

			logger, err := NewLogger(store, q, 1, time.Hour)
			if err != nil {
				t.Fatalf("new logger: %v", err)
			}

			var evaluateCalls int
			var redactCalls int
			logger.SetEvaluator(evaluatorFunc(func(e *AuditEvent) EvalResult {
				evaluateCalls++
				return EvalResult{Evaluated: []string{"rule"}, Triggered: []TriggeredRule{{Name: "rule", Enforcement: "warn"}}}
			}))
			logger.SetRedactor(redactorFunc(func(e *AuditEvent, ruleTriggered bool) {
				redactCalls++
			}))

			ctx, cancel := context.WithCancel(context.Background())
			logger.Start(ctx)
			defer func() {
				cancel()
				logger.Stop()
			}()

			q.Send(&AuditEvent{
				ID:         "evt_exempt",
				Timestamp:  time.Now().UTC(),
				AgentID:    "crabwise",
				ActionType: ActionSystem,
				Action:     action,
				Outcome:    OutcomeSuccess,
			})

			batch := waitInsertedBatch(t, store)
			if len(batch) != 1 {
				t.Fatalf("expected 1 event, got %d", len(batch))
			}

			if evaluateCalls != 0 || redactCalls != 0 {
				t.Fatalf("expected exempt system event to skip evaluator/redactor, got evaluate=%d redact=%d", evaluateCalls, redactCalls)
			}
			if batch[0].CommandmentsEvaluated != "" || batch[0].CommandmentsTriggered != "" {
				t.Fatalf("expected exempt event commandments fields to be empty")
			}
		})
	}
}

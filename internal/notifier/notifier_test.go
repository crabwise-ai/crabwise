package notifier

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/queue"
)

type mockBackend struct {
	mu     sync.Mutex
	events []*audit.AuditEvent
}

func (m *mockBackend) Name() string { return "mock" }
func (m *mockBackend) Send(_ context.Context, evt *audit.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evt)
	return nil
}
func (m *mockBackend) Events() []*audit.AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*audit.AuditEvent(nil), m.events...)
}

type testStore struct {
	mu     sync.Mutex
	events []*audit.AuditEvent
}

func (s *testStore) InsertEvents(events []*audit.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, events...)
	return nil
}
func (s *testStore) GetLastEventHash() (string, error) { return "", nil }
func (s *testStore) InsertChainAnchor(_, _, _ string) error { return nil }

func TestNotifier_OnlyBlockedEvents(t *testing.T) {
	q := queue.New(1000, queue.PolicyDropOldest, 0)
	store := &testStore{}
	logger, err := audit.NewLogger(store, q, 10, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger.Start(ctx)
	defer logger.Stop()

	mock := &mockBackend{}
	n := &Notifier{
		logger:   logger,
		backends: []Backend{mock},
	}
	n.Start(ctx)
	defer n.Stop()

	// Send events with different outcomes
	outcomes := []audit.Outcome{
		audit.OutcomeSuccess,
		audit.OutcomeWarned,
		audit.OutcomeBlocked,
		audit.OutcomeFailure,
		audit.OutcomeBlocked,
	}
	for i, outcome := range outcomes {
		evt := &audit.AuditEvent{
			ID:        fmt.Sprintf("evt_%d", i),
			Timestamp: time.Now().UTC(),
			AgentID:   "test",
			Action:    fmt.Sprintf("action_%d", i),
			Outcome:   outcome,
		}
		q.Send(evt)
	}

	// Wait for flush + notification propagation
	time.Sleep(300 * time.Millisecond)

	got := mock.Events()
	if len(got) != 2 {
		t.Fatalf("expected 2 blocked events, got %d", len(got))
	}
	for _, e := range got {
		if e.Outcome != audit.OutcomeBlocked {
			t.Errorf("expected blocked outcome, got %s", e.Outcome)
		}
	}
}

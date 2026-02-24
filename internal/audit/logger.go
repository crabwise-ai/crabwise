package audit

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/crabwise-ai/crabwise/internal/queue"
)

// EventStore persists audit events.
type EventStore interface {
	InsertEvents(events []*AuditEvent) error
	GetLastEventHash() (string, error)
	InsertChainAnchor(epoch, eventID, eventHash string) error
}

type Evaluator interface {
	Evaluate(e *AuditEvent) EvalResult
}

type Redactor interface {
	Redact(e *AuditEvent, ruleTriggered bool)
}

type EvalResult struct {
	Evaluated []string
	Triggered []TriggeredRule
}

type TriggeredRule struct {
	Name        string `json:"name"`
	Enforcement string `json:"enforcement"`
	Message     string `json:"message,omitempty"`
}

type Logger struct {
	store     EventStore
	q         *queue.Queue
	batchSize int
	flushInt  time.Duration
	prevHash  string

	subscribers map[chan *AuditEvent]struct{}
	subMu       sync.RWMutex

	cancel context.CancelFunc
	wg     sync.WaitGroup

	evaluator Evaluator
	redactor  Redactor
}

func NewLogger(store EventStore, q *queue.Queue, batchSize int, flushInterval time.Duration) (*Logger, error) {
	prevHash, err := store.GetLastEventHash()
	if err != nil {
		return nil, err
	}

	return &Logger{
		store:       store,
		q:           q,
		batchSize:   batchSize,
		flushInt:    flushInterval,
		prevHash:    prevHash,
		subscribers: make(map[chan *AuditEvent]struct{}),
	}, nil
}

func (l *Logger) Start(ctx context.Context) {
	ctx, l.cancel = context.WithCancel(ctx)

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.serializerLoop(ctx)
	}()
}

func (l *Logger) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
	l.wg.Wait()
}

func (l *Logger) SetEvaluator(evaluator Evaluator) {
	l.evaluator = evaluator
}

func (l *Logger) SetRedactor(redactor Redactor) {
	l.redactor = redactor
}

// Subscribe returns a channel that receives new audit events.
func (l *Logger) Subscribe() chan *AuditEvent {
	ch := make(chan *AuditEvent, 256)
	l.subMu.Lock()
	l.subscribers[ch] = struct{}{}
	l.subMu.Unlock()
	return ch
}

func (l *Logger) Unsubscribe(ch chan *AuditEvent) {
	l.subMu.Lock()
	delete(l.subscribers, ch)
	l.subMu.Unlock()
	close(ch)
}

func (l *Logger) serializerLoop(ctx context.Context) {
	ticker := time.NewTicker(l.flushInt)
	defer ticker.Stop()

	var batch []*AuditEvent
	lastEpoch := ""

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := l.store.InsertEvents(batch); err != nil {
			log.Printf("audit: flush error: %v", err)
			return
		}

		// Notify subscribers
		l.subMu.RLock()
		for _, e := range batch {
			for ch := range l.subscribers {
				select {
				case ch <- e:
				default:
				}
			}
		}
		l.subMu.RUnlock()

		batch = batch[:0]
	}

	processEvent := func(e *AuditEvent) {
		if !isCommandmentsExemptSystemEvent(e) {
			result := EvalResult{
				Evaluated: []string{},
				Triggered: []TriggeredRule{},
			}
			if l.evaluator != nil {
				result = l.evaluator.Evaluate(e)
				if result.Evaluated == nil {
					result.Evaluated = []string{}
				}
				if result.Triggered == nil {
					result.Triggered = []TriggeredRule{}
				}
			}

			if b, err := json.Marshal(result.Evaluated); err == nil {
				e.CommandmentsEvaluated = string(b)
			} else {
				log.Printf("audit: failed to encode commandments_evaluated: %v", err)
			}
			if b, err := json.Marshal(result.Triggered); err == nil {
				e.CommandmentsTriggered = string(b)
			} else {
				log.Printf("audit: failed to encode commandments_triggered: %v", err)
			}

			for _, triggeredRule := range result.Triggered {
				candidate := outcomeForEnforcement(triggeredRule.Enforcement, e.AdapterType)
				if outcomeRank(candidate) > outcomeRank(e.Outcome) {
					e.Outcome = candidate
				}
			}

			if l.redactor != nil {
				l.redactor.Redact(e, len(result.Triggered) > 0)
			}
		}

		e.PrevHash = l.prevHash
		e.EventHash = ComputeHash(e, l.prevHash)
		l.prevHash = e.EventHash

		epoch := e.Timestamp.UTC().Format("2006-01-02")
		if epoch != lastEpoch {
			if err := l.store.InsertChainAnchor(epoch, e.ID, e.EventHash); err != nil {
				log.Printf("audit: anchor error: %v", err)
			}
			lastEpoch = epoch
		}

		batch = append(batch, e)

		if len(batch) >= l.batchSize {
			flush()
		}
	}

	processOverflow := func(oe queue.OverflowEvent) {
		e := &AuditEvent{
			ID:         oe.ID,
			Timestamp:  oe.Timestamp,
			AgentID:    "crabwise",
			ActionType: ActionSystem,
			Action:     "pipeline_overflow",
			Outcome:    OutcomeWarned,
		}
		processEvent(e)
	}

	// drainControl processes all pending control channel events.
	// Called on every iteration to prevent starvation when main channel is hot.
	drainControl := func() {
		for {
			select {
			case oe, ok := <-l.q.Control():
				if !ok {
					return
				}
				processOverflow(oe)
			default:
				return
			}
		}
	}

	for {
		// Always drain control channel first — overflow events must not be starved
		drainControl()

		select {
		case <-ctx.Done():
			// Drain both channels on shutdown
			for {
				select {
				case item, ok := <-l.q.Receive():
					if !ok {
						drainControl()
						flush()
						return
					}
					if e, ok := item.(*AuditEvent); ok {
						processEvent(e)
					}
				case oe, ok := <-l.q.Control():
					if !ok {
						continue
					}
					processOverflow(oe)
				default:
					drainControl()
					flush()
					return
				}
			}

		case item, ok := <-l.q.Receive():
			if !ok {
				flush()
				return
			}
			if e, ok := item.(*AuditEvent); ok {
				processEvent(e)
			}

		case oe, ok := <-l.q.Control():
			if !ok {
				continue
			}
			processOverflow(oe)

		case <-ticker.C:
			flush()
		}
	}
}

func outcomeForEnforcement(enforcement, adapterType string) Outcome {
	switch strings.ToLower(enforcement) {
	case "warn":
		return OutcomeWarned
	case "block":
		if adapterType == "log_watcher" {
			return OutcomeWarned
		}
		return OutcomeBlocked
	default:
		return OutcomeSuccess
	}
}

func outcomeRank(outcome Outcome) int {
	switch outcome {
	case OutcomeSuccess:
		return 0
	case OutcomeWarned:
		return 1
	case OutcomeFailure:
		return 2
	case OutcomeBlocked:
		return 3
	default:
		return -1
	}
}

func isCommandmentsExemptSystemEvent(e *AuditEvent) bool {
	if e == nil || e.ActionType != ActionSystem {
		return false
	}
	switch e.Action {
	case "commandments_reload_ok", "commandments_reload_failed", "commandments_load_failed", "commandments_load_ok", "tool_registry_reload_ok", "tool_registry_reload_failed":
		return true
	default:
		return false
	}
}

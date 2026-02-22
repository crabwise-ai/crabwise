package queue

import (
	"sync"
	"sync/atomic"
	"time"
)

type OverflowPolicy string

const (
	PolicyBlockWithTimeout OverflowPolicy = "block_with_timeout"
	PolicyDropOldest       OverflowPolicy = "drop_oldest"
)

type Stats struct {
	Depth    int
	Capacity int
	Dropped  uint64
	Overflow uint64
}

// OverflowEvent is emitted on the control channel when overflow occurs.
type OverflowEvent struct {
	ID        string
	Timestamp time.Time
}

type Queue struct {
	ch       chan any
	controlC chan OverflowEvent
	policy   OverflowPolicy
	timeout  time.Duration
	dropped  atomic.Uint64
	overflow atomic.Uint64
	closed   atomic.Bool
	mu       sync.Mutex
}

func New(capacity int, policy OverflowPolicy, blockTimeout time.Duration) *Queue {
	return &Queue{
		ch:       make(chan any, capacity),
		controlC: make(chan OverflowEvent, 100),
		policy:   policy,
		timeout:  blockTimeout,
	}
}

func (q *Queue) Send(event any) bool {
	if q.closed.Load() {
		return false
	}

	switch q.policy {
	case PolicyDropOldest:
		return q.sendDropOldest(event)
	default:
		return q.sendBlockTimeout(event)
	}
}

func (q *Queue) sendBlockTimeout(event any) bool {
	select {
	case q.ch <- event:
		return true
	default:
	}

	timer := time.NewTimer(q.timeout)
	defer timer.Stop()

	select {
	case q.ch <- event:
		return true
	case <-timer.C:
		q.dropped.Add(1)
		q.overflow.Add(1)
		q.emitOverflow()
		return false
	}
}

func (q *Queue) sendDropOldest(event any) bool {
	select {
	case q.ch <- event:
		return true
	default:
	}

	q.mu.Lock()
	select {
	case <-q.ch:
		q.dropped.Add(1)
	default:
	}
	q.mu.Unlock()

	select {
	case q.ch <- event:
		q.overflow.Add(1)
		q.emitOverflow()
		return true
	default:
		q.dropped.Add(1)
		q.overflow.Add(1)
		q.emitOverflow()
		return false
	}
}

func (q *Queue) emitOverflow() {
	evt := OverflowEvent{
		ID:        "overflow_" + time.Now().UTC().Format("20060102T150405.000"),
		Timestamp: time.Now().UTC(),
	}
	select {
	case q.controlC <- evt:
	default:
	}
}

// Receive returns the main event channel for consumers.
func (q *Queue) Receive() <-chan any {
	return q.ch
}

// Control returns the control channel for overflow events.
func (q *Queue) Control() <-chan OverflowEvent {
	return q.controlC
}

func (q *Queue) Stats() Stats {
	return Stats{
		Depth:    len(q.ch),
		Capacity: cap(q.ch),
		Dropped:  q.dropped.Load(),
		Overflow: q.overflow.Load(),
	}
}

func (q *Queue) Close() {
	if q.closed.CompareAndSwap(false, true) {
		close(q.ch)
		close(q.controlC)
	}
}

// Drain reads remaining events from the queue after Close.
func (q *Queue) Drain() []any {
	var events []any
	for e := range q.ch {
		events = append(events, e)
	}
	return events
}

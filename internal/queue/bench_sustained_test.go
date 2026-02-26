//go:build m3_bench

package queue

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestEventLossUnderNominalLoad(t *testing.T) {
	t.Log("m3_bench_profile event_loss")

	q := New(10000, PolicyBlockWithTimeout, 100*time.Millisecond)

	const (
		eventsPerSec = 100
		duration     = 15 * time.Second
	)

	// Consumer goroutine
	var received atomic.Int64
	done := make(chan struct{})
	go func() {
		for range q.Receive() {
			received.Add(1)
		}
		close(done)
	}()

	// Producer: 100 events/sec
	ticker := time.NewTicker(time.Second / eventsPerSec)
	defer ticker.Stop()
	sent := 0
	deadline := time.After(duration)

	for {
		select {
		case <-deadline:
			goto finish
		case <-ticker.C:
			q.Send(sent)
			sent++
		}
	}
finish:

	q.Close()
	<-done

	stats := q.Stats()
	recv := received.Load()

	t.Logf("m3_bench event_loss sent=%d received=%d dropped=%d overflow=%d",
		sent, recv, stats.Dropped, stats.Overflow)

	if stats.Dropped > 0 {
		t.Fatalf("expected 0 drops under nominal load, got %d", stats.Dropped)
	}
	if int64(sent) != recv {
		t.Fatalf("event loss: sent=%d received=%d", sent, recv)
	}
}

func TestQueueSaturationBehavior(t *testing.T) {
	t.Log("m3_bench_profile queue_saturation")

	q := New(100, PolicyBlockWithTimeout, 1*time.Millisecond)

	// Consumer: intentionally slow
	var received atomic.Int64
	done := make(chan struct{})
	go func() {
		for range q.Receive() {
			received.Add(1)
			time.Sleep(10 * time.Millisecond) // slow consumer
		}
		close(done)
	}()

	// Producer: burst 500 events fast
	sent := 0
	for i := 0; i < 500; i++ {
		q.Send(i)
		sent++
	}

	// Wait a bit for drain
	time.Sleep(200 * time.Millisecond)
	q.Close()
	<-done

	stats := q.Stats()

	t.Logf("m3_bench queue_saturation sent=%d received=%d dropped=%d overflow=%d",
		sent, received.Load(), stats.Dropped, stats.Overflow)

	// Should have experienced overflow
	if stats.Overflow == 0 {
		t.Fatal("expected overflow events under saturation, got 0")
	}
	// Should not panic or deadlock (test completing is the assertion)
}

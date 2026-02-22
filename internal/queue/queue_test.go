package queue

import (
	"testing"
	"time"
)

func TestQueue_SendReceive(t *testing.T) {
	q := New(10, PolicyBlockWithTimeout, 100*time.Millisecond)

	q.Send("e1")
	q.Send("e2")

	stats := q.Stats()
	if stats.Depth != 2 {
		t.Fatalf("expected depth 2, got %d", stats.Depth)
	}

	e := <-q.Receive()
	if e.(string) != "e1" {
		t.Fatalf("expected e1, got %v", e)
	}
}

func TestQueue_BlockWithTimeout_Overflow(t *testing.T) {
	q := New(2, PolicyBlockWithTimeout, 10*time.Millisecond)

	q.Send("e1")
	q.Send("e2")
	ok := q.Send("e3") // should timeout and drop

	if ok {
		t.Fatal("expected send to fail on full queue with timeout")
	}

	stats := q.Stats()
	if stats.Dropped != 1 {
		t.Fatalf("expected 1 dropped, got %d", stats.Dropped)
	}

	// Control channel should have overflow event
	select {
	case evt := <-q.Control():
		if evt.ID == "" {
			t.Fatal("expected overflow event ID")
		}
	case <-time.After(time.Second):
		t.Fatal("expected overflow event on control channel")
	}
}

func TestQueue_DropOldest(t *testing.T) {
	q := New(2, PolicyDropOldest, 10*time.Millisecond)

	q.Send("e1")
	q.Send("e2")
	ok := q.Send("e3") // should drop e1

	if !ok {
		t.Fatal("drop_oldest should succeed")
	}

	stats := q.Stats()
	if stats.Dropped != 1 {
		t.Fatalf("expected 1 dropped, got %d", stats.Dropped)
	}
}

func TestQueue_CloseAndDrain(t *testing.T) {
	q := New(10, PolicyBlockWithTimeout, 100*time.Millisecond)

	q.Send("e1")
	q.Send("e2")

	q.Close()

	ok := q.Send("e3")
	if ok {
		t.Fatal("send after close should fail")
	}

	events := q.Drain()
	if len(events) != 2 {
		t.Fatalf("expected 2 drained events, got %d", len(events))
	}
}

func TestQueue_Stats(t *testing.T) {
	q := New(100, PolicyBlockWithTimeout, 100*time.Millisecond)

	stats := q.Stats()
	if stats.Capacity != 100 {
		t.Fatalf("expected capacity 100, got %d", stats.Capacity)
	}
	if stats.Depth != 0 {
		t.Fatalf("expected depth 0, got %d", stats.Depth)
	}
}

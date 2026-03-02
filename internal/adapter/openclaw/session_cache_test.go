package openclaw

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionCacheRefresh(t *testing.T) {
	t.Parallel()

	cache := NewSessionCache(&fakeSessionLister{
		sessions: []SessionInfo{
			{
				Key:            "agent:main:discord:channel:123",
				AgentID:        "main",
				LastActivityAt: 1730000001000,
				Model:          "claude-sonnet",
			},
		},
	}, time.Minute)

	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, ok := cache.Get("agent:main:discord:channel:123")
	if !ok {
		t.Fatal("expected session to be present after refresh")
	}
	if got.AgentID != "main" {
		t.Fatalf("expected agent id main, got %q", got.AgentID)
	}
	if got.Model != "claude-sonnet" {
		t.Fatalf("expected model claude-sonnet, got %q", got.Model)
	}
}

func TestSessionCacheRefresh_PropagatesErrors(t *testing.T) {
	t.Parallel()

	cache := NewSessionCache(&fakeSessionLister{err: errors.New("boom")}, time.Minute)
	if err := cache.Refresh(context.Background()); err == nil {
		t.Fatal("expected refresh to return error")
	}
}

type fakeSessionLister struct {
	sessions []SessionInfo
	err      error
}

func (f *fakeSessionLister) ListSessions(context.Context) ([]SessionInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.sessions, nil
}

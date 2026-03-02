package openclaw

import (
	"context"
	"sync"
	"time"
)

type SessionLister interface {
	ListSessions(ctx context.Context) ([]SessionInfo, error)
}

type SessionCache struct {
	client          SessionLister
	refreshInterval time.Duration

	mu       sync.RWMutex
	sessions map[string]SessionInfo
}

func NewSessionCache(client SessionLister, refreshInterval time.Duration) *SessionCache {
	return &SessionCache{
		client:          client,
		refreshInterval: refreshInterval,
		sessions:        make(map[string]SessionInfo),
	}
}

func (c *SessionCache) Refresh(ctx context.Context) error {
	sessions, err := c.client.ListSessions(ctx)
	if err != nil {
		return err
	}

	next := make(map[string]SessionInfo, len(sessions))
	for _, session := range sessions {
		next[session.Key] = session
	}

	c.mu.Lock()
	c.sessions = next
	c.mu.Unlock()

	return nil
}

func (c *SessionCache) Get(key string) (SessionInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	session, ok := c.sessions[key]
	return session, ok
}

func (c *SessionCache) Snapshot() map[string]SessionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]SessionInfo, len(c.sessions))
	for key, session := range c.sessions {
		out[key] = session
	}
	return out
}

func (c *SessionCache) RefreshInterval() time.Duration {
	return c.refreshInterval
}

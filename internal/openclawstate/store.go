package openclawstate

import (
	"math"
	"sync"
	"time"
)

const maxRecentChats = 256

type Store struct {
	correlationWindow time.Duration

	mu        sync.RWMutex
	sessions  map[string]SessionMeta
	runs      map[string]string
	chats     []RecentChat
	matches   uint64
	ambiguous uint64
}

func New(correlationWindow time.Duration) *Store {
	return &Store{
		correlationWindow: correlationWindow,
		sessions:          make(map[string]SessionMeta),
		runs:              make(map[string]string),
	}
}

func (s *Store) RecordSession(meta SessionMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[meta.SessionKey] = meta
}

func (s *Store) RecordRun(runID, sessionKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[runID] = sessionKey
}

func (s *Store) RecordChat(runID, sessionKey, provider, model string, ts time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if runID != "" {
		s.runs[runID] = sessionKey
	}

	s.chats = append(s.chats, RecentChat{
		RunID:      runID,
		SessionKey: sessionKey,
		Provider:   provider,
		Model:      model,
		Timestamp:  ts,
	})
	if len(s.chats) > maxRecentChats {
		s.chats = append([]RecentChat(nil), s.chats[len(s.chats)-maxRecentChats:]...)
	}
}

func (s *Store) MatchProxyRequest(ts time.Time, provider, model string) (MatchResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bestIndex := -1
	bestScore := math.MinInt
	bestDelta := time.Duration(1<<63 - 1)
	ambiguous := false

	for i, chat := range s.chats {
		delta := absDuration(ts.Sub(chat.Timestamp))
		if delta > s.correlationWindow {
			continue
		}

		score := 0
		if provider != "" && chat.Provider == provider {
			score += 2
		}
		if model != "" && chat.Model == model {
			score += 1
		}

		if score > bestScore || (score == bestScore && delta < bestDelta) {
			bestIndex = i
			bestScore = score
			bestDelta = delta
			ambiguous = false
			continue
		}
		if score == bestScore && delta == bestDelta {
			ambiguous = true
		}
	}

	if bestIndex < 0 || ambiguous {
		if ambiguous {
			s.ambiguous++
		}
		return MatchResult{}, false
	}

	chat := s.chats[bestIndex]
	meta := s.sessions[chat.SessionKey]
	s.matches++

	return MatchResult{
		AgentID:       "openclaw",
		SessionKey:    chat.SessionKey,
		ParentSession: meta.ParentSession,
		RunID:         chat.RunID,
		Provider:      chat.Provider,
		Model:         firstNonEmpty(chat.Model, meta.Model),
		ThinkingLevel: meta.ThinkingLevel,
	}, true
}

type Stats struct {
	SessionCount int
	RunCount     int
	RecentChats  int
	Matches      uint64
	Ambiguous    uint64
}

func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		SessionCount: len(s.sessions),
		RunCount:     len(s.runs),
		RecentChats:  len(s.chats),
		Matches:      s.matches,
		Ambiguous:    s.ambiguous,
	}
}

func (s *Store) Snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make(map[string]SessionMeta, len(s.sessions))
	for key, meta := range s.sessions {
		sessions[key] = meta
	}

	runs := make(map[string]string, len(s.runs))
	for runID, sessionKey := range s.runs {
		runs[runID] = sessionKey
	}

	chats := append([]RecentChat(nil), s.chats...)

	return map[string]any{
		"sessions": sessions,
		"runs":     runs,
		"chats":    chats,
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

package openclawstate

import "time"

type SessionMeta struct {
	SessionKey    string
	AgentID       string
	ParentSession string
	Model         string
	ThinkingLevel string
	LastActivity  time.Time
}

type RecentChat struct {
	RunID      string
	SessionKey string
	Provider   string
	Model      string
	Timestamp  time.Time
}

type MatchResult struct {
	AgentID       string
	SessionKey    string
	ParentSession string
	RunID         string
	Provider      string
	Model         string
	ThinkingLevel string
}

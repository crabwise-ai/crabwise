package openclaw

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func mapChatEvent(ts time.Time, payload *ChatEvent, session SessionInfo, provider, model string) *audit.AuditEvent {
	args, _ := json.Marshal(map[string]interface{}{
		"openclaw.run_id":      payload.RunID,
		"openclaw.session_key": payload.SessionKey,
		"openclaw.state":       payload.State,
	})

	return &audit.AuditEvent{
		ID:              uuid.NewString(),
		Timestamp:       ts,
		AgentID:         "openclaw",
		ActionType:      audit.ActionAIRequest,
		Action:          "chat",
		Arguments:       string(args),
		SessionID:       payload.SessionKey,
		ParentSessionID: session.SpawnedBy,
		Outcome:         audit.OutcomeSuccess,
		Provider:        provider,
		Model:           model,
		InputTokens:     tokenCount(payload.Usage, true),
		OutputTokens:    tokenCount(payload.Usage, false),
		AdapterID:       "openclaw-gateway",
		AdapterType:     "openclaw",
	}
}

func mapAgentEvent(payload *AgentEvent) *audit.AuditEvent {
	args, _ := json.Marshal(map[string]interface{}{
		"openclaw.run_id":      payload.RunID,
		"openclaw.session_key": payload.SessionKey,
		"stream":               payload.Stream,
		"data":                 payload.Data,
	})

	return &audit.AuditEvent{
		ID:          uuid.NewString(),
		Timestamp:   time.UnixMilli(payload.TS),
		AgentID:     "openclaw",
		ActionType:  audit.ActionSystem,
		Action:      "agent",
		Arguments:   string(args),
		SessionID:   payload.SessionKey,
		Outcome:     audit.OutcomeSuccess,
		AdapterID:   "openclaw-gateway",
		AdapterType: "openclaw",
	}
}

func inferProviderFromModel(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "claude"):
		return "anthropic"
	case strings.Contains(lower, "gpt"), strings.Contains(lower, "o1"), strings.Contains(lower, "o3"), strings.Contains(lower, "o4"):
		return "openai"
	case strings.Contains(lower, "gemini"):
		return "google"
	default:
		return ""
	}
}

func tokenCount(usage *TokenUsage, input bool) int64 {
	if usage == nil {
		return 0
	}
	if input {
		return usage.InputTokens
	}
	return usage.OutputTokens
}

func sessionKeyModel(string) string {
	return ""
}

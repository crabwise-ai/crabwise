package audit

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

type ActionType string

const (
	ActionToolCall         ActionType = "tool_call"
	ActionFileAccess       ActionType = "file_access"
	ActionCommandExecution ActionType = "command_execution"
	ActionAIRequest        ActionType = "ai_request"
	ActionSystem           ActionType = "system"
	ActionUnknown          ActionType = "unknown"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeBlocked Outcome = "blocked"
	OutcomeWarned  Outcome = "warned"
)

// AuditEvent is the shared event contract for all adapters.
// Field order is canonical — the hash serializer enumerates fields in this declared order.
type AuditEvent struct {
	ID                    string     `json:"id"`
	Timestamp             time.Time  `json:"timestamp"`
	AgentID               string     `json:"agent_id"`
	AgentPID              int        `json:"agent_pid,omitempty"`
	ActionType            ActionType `json:"action_type"`
	Action                string     `json:"action,omitempty"`
	Arguments             string     `json:"arguments,omitempty"` // JSON
	SessionID             string     `json:"session_id,omitempty"`
	ParentSessionID       string     `json:"parent_session_id,omitempty"`
	WorkingDir            string     `json:"working_dir,omitempty"`
	ParserVersion         string     `json:"parser_version,omitempty"`
	Outcome               Outcome    `json:"outcome"`
	CommandmentsEvaluated string     `json:"commandments_evaluated,omitempty"` // JSON array
	CommandmentsTriggered string     `json:"commandments_triggered,omitempty"` // JSON array
	Provider              string     `json:"provider,omitempty"`
	Model                 string     `json:"model,omitempty"`
	ToolCategory          string     `json:"tool_category,omitempty"`
	ToolEffect            string     `json:"tool_effect,omitempty"`
	ToolName              string     `json:"tool_name,omitempty"`
	TaxonomyVersion       string     `json:"taxonomy_version,omitempty"`
	ClassificationSource  string     `json:"classification_source,omitempty"`
	InputTokens           int64      `json:"input_tokens,omitempty"`
	OutputTokens          int64      `json:"output_tokens,omitempty"`
	AdapterID             string     `json:"adapter_id,omitempty"`
	AdapterType           string     `json:"adapter_type,omitempty"`
	RawPayloadRef         string     `json:"raw_payload_ref,omitempty"`
	Hostname              string     `json:"hostname,omitempty"`
	UserID                string     `json:"user_id,omitempty"`
	PrevHash              string     `json:"prev_hash,omitempty"`
	EventHash             string     `json:"event_hash,omitempty"`
	Redacted              bool       `json:"redacted,omitempty"`

	// SourceFile and SourceOffset are transport-only metadata for atomic offset commits.
	// Not persisted to DB, not included in hash computation.
	SourceFile   string `json:"-"`
	SourceOffset int64  `json:"-"`
}

// CanonicalBytes produces deterministic bytes for hash computation.
// Fields are serialized in declared struct order. EventHash is excluded.
func CanonicalBytes(e *AuditEvent) []byte {
	var buf []byte

	buf = appendString(buf, e.ID)
	buf = appendTime(buf, e.Timestamp)
	buf = appendString(buf, e.AgentID)
	buf = appendInt(buf, int64(e.AgentPID))
	buf = appendString(buf, string(e.ActionType))
	buf = appendString(buf, e.Action)
	buf = appendString(buf, e.Arguments)
	buf = appendString(buf, e.SessionID)
	buf = appendString(buf, e.ParentSessionID)
	buf = appendString(buf, e.WorkingDir)
	buf = appendString(buf, e.ParserVersion)
	buf = appendString(buf, string(e.Outcome))
	buf = appendString(buf, e.CommandmentsEvaluated)
	buf = appendString(buf, e.CommandmentsTriggered)
	buf = appendString(buf, e.Provider)
	buf = appendString(buf, e.Model)
	buf = appendString(buf, e.ToolCategory)
	buf = appendString(buf, e.ToolEffect)
	buf = appendString(buf, e.ToolName)
	buf = appendString(buf, e.TaxonomyVersion)
	buf = appendString(buf, e.ClassificationSource)
	buf = appendInt(buf, e.InputTokens)
	buf = appendInt(buf, e.OutputTokens)
	buf = appendString(buf, e.AdapterID)
	buf = appendString(buf, e.AdapterType)
	buf = appendString(buf, e.RawPayloadRef)
	buf = appendString(buf, e.Hostname)
	buf = appendString(buf, e.UserID)
	buf = appendString(buf, e.PrevHash)
	// EventHash excluded — it's the output
	buf = appendBool(buf, e.Redacted)

	return buf
}

// ComputeHash computes SHA-256 over canonical event bytes + previous hash.
func ComputeHash(e *AuditEvent, prevHash string) string {
	canonical := CanonicalBytes(e)
	h := sha256.New()
	h.Write(canonical)
	h.Write([]byte(prevHash))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func appendString(buf []byte, s string) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(len(s)))
	buf = append(buf, b...)
	return append(buf, s...)
}

func appendInt(buf []byte, v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return append(buf, b...)
}

func appendTime(buf []byte, t time.Time) []byte {
	return appendString(buf, t.UTC().Format(time.RFC3339Nano))
}

func appendBool(buf []byte, v bool) []byte {
	if v {
		return append(buf, 1)
	}
	return append(buf, 0)
}

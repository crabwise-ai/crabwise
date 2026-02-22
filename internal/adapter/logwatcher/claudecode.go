package logwatcher

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

// uuid is no longer used — file-sourced events use deterministic IDs.
// Non-file events (overflow, etc.) are created in other packages.

// deterministicID generates a stable event ID from source file and byte offset.
// This ensures INSERT OR IGNORE deduplicates on restart after partial flush.
// suffix disambiguates multiple events from the same line (e.g. multiple tool_use blocks).
func deterministicID(sourceFile string, lineOffset int64, suffix int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", sourceFile, lineOffset, suffix)))
	return fmt.Sprintf("%x", h[:12])
}

const ParserVersion = "cc-parser-v0.1"

// CCRecord represents a raw Claude Code JSONL record.
type CCRecord struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	UUID      string          `json:"uuid"`
	ParentUUID string         `json:"parentUuid"`
	CWD       string          `json:"cwd"`
	Version   string          `json:"version"`
	Message   json.RawMessage `json:"message"`

	// Tool result fields
	ToolUseResult          string `json:"toolUseResult"`
	SourceToolAssistantUUID string `json:"sourceToolAssistantUUID"`

	// Raw bytes for unknown record handling
	rawBytes []byte
}

type CCMessage struct {
	Role    string      `json:"role"`
	Model   string      `json:"model"`
	Content interface{} `json:"content"` // can be string or []ContentBlock
	Usage   *CCUsage    `json:"usage"`
}

type ContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Text  string          `json:"text"`
	Input json.RawMessage `json:"input"`
}

type CCUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// ParseResult holds parsed events and parse statistics.
type ParseResult struct {
	Events       []*audit.AuditEvent
	Skipped      int
	Errors       int
	Unknown      int
	Total        int
}

// ParseLine parses a single JSONL line into audit events.
// lineOffset is the byte offset of the start of this line in the source file,
// used for deterministic event IDs.
func ParseLine(line []byte, sessionFile string, lineOffset int64) ([]*audit.AuditEvent, error) {
	if len(line) == 0 {
		return nil, nil
	}

	var rec CCRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		// Malformed JSON — emit unknown event
		return []*audit.AuditEvent{unknownEvent(sessionFile, line, err, lineOffset)}, nil
	}
	rec.rawBytes = line

	switch rec.Type {
	case "assistant":
		return parseAssistant(&rec, sessionFile, lineOffset)
	case "user":
		return parseUser(&rec, sessionFile)
	case "system":
		return []*audit.AuditEvent{systemEvent(&rec, sessionFile, lineOffset)}, nil
	case "queue-operation", "file-history-snapshot", "summary":
		return nil, nil // skip
	case "":
		return []*audit.AuditEvent{unknownEvent(sessionFile, line, fmt.Errorf("empty type"), lineOffset)}, nil
	default:
		return []*audit.AuditEvent{unknownEvent(sessionFile, line, fmt.Errorf("unknown type: %s", rec.Type), lineOffset)}, nil
	}
}

func parseAssistant(rec *CCRecord, sessionFile string, lineOffset int64) ([]*audit.AuditEvent, error) {
	var msg CCMessage
	if err := json.Unmarshal(rec.Message, &msg); err != nil {
		return []*audit.AuditEvent{unknownEvent(sessionFile, rec.rawBytes, err, lineOffset)}, nil
	}

	var events []*audit.AuditEvent

	// Parse content blocks for tool_use
	blocks := parseContentBlocks(msg.Content)
	toolIdx := 0
	for _, block := range blocks {
		if block.Type == "tool_use" {
			evt := toolCallEvent(rec, &msg, &block, sessionFile, lineOffset, toolIdx)
			events = append(events, evt)
			toolIdx++
		}
	}

	// If message has usage info and no tool calls, it's an AI request
	if msg.Usage != nil && len(events) == 0 {
		evt := aiRequestEvent(rec, &msg, sessionFile, lineOffset)
		events = append(events, evt)
	}

	// If usage info present and tool calls exist, attach usage to first tool call
	if msg.Usage != nil && len(events) > 0 {
		events[0].InputTokens = msg.Usage.InputTokens
		events[0].OutputTokens = msg.Usage.OutputTokens
		events[0].Model = msg.Model
	}

	return events, nil
}

func parseUser(rec *CCRecord, sessionFile string) ([]*audit.AuditEvent, error) {
	// Tool results — we skip these as standalone events since
	// the tool call event already captures the action
	if rec.ToolUseResult != "" {
		return nil, nil
	}
	return nil, nil
}

func parseContentBlocks(content interface{}) []ContentBlock {
	if content == nil {
		return nil
	}

	// Content can be a string or an array of content blocks
	raw, err := json.Marshal(content)
	if err != nil {
		return nil
	}

	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	return blocks
}

func toolCallEvent(rec *CCRecord, msg *CCMessage, block *ContentBlock, sessionFile string, lineOffset int64, toolIdx int) *audit.AuditEvent {
	actionType := classifyTool(block.Name)

	var args string
	if len(block.Input) > 0 {
		args = string(block.Input)
	}

	ts := parseTimestamp(rec.Timestamp)
	sessionID := extractSessionIDFromFile(sessionFile)

	return &audit.AuditEvent{
		ID:            deterministicID(sessionFile, lineOffset, toolIdx),
		Timestamp:     ts,
		AgentID:       "claude-code",
		ActionType:    actionType,
		Action:        block.Name,
		Arguments:     args,
		SessionID:     sessionID,
		WorkingDir:    rec.CWD,
		ParserVersion: ParserVersion,
		Outcome:       audit.OutcomeSuccess,
		Model:         msg.Model,
		AdapterID:     "log-watcher",
		AdapterType:   "log_watcher",
	}
}

func aiRequestEvent(rec *CCRecord, msg *CCMessage, sessionFile string, lineOffset int64) *audit.AuditEvent {
	ts := parseTimestamp(rec.Timestamp)
	sessionID := extractSessionIDFromFile(sessionFile)

	return &audit.AuditEvent{
		ID:            deterministicID(sessionFile, lineOffset, 0),
		Timestamp:     ts,
		AgentID:       "claude-code",
		ActionType:    audit.ActionAIRequest,
		Action:        "chat",
		SessionID:     sessionID,
		WorkingDir:    rec.CWD,
		ParserVersion: ParserVersion,
		Outcome:       audit.OutcomeSuccess,
		Model:         msg.Model,
		InputTokens:   msg.Usage.InputTokens,
		OutputTokens:  msg.Usage.OutputTokens,
		AdapterID:     "log-watcher",
		AdapterType:   "log_watcher",
	}
}

func systemEvent(rec *CCRecord, sessionFile string, lineOffset int64) *audit.AuditEvent {
	ts := parseTimestamp(rec.Timestamp)
	sessionID := extractSessionIDFromFile(sessionFile)

	return &audit.AuditEvent{
		ID:            deterministicID(sessionFile, lineOffset, 0),
		Timestamp:     ts,
		AgentID:       "claude-code",
		ActionType:    audit.ActionSystem,
		Action:        "system",
		SessionID:     sessionID,
		ParserVersion: ParserVersion,
		Outcome:       audit.OutcomeSuccess,
		AdapterID:     "log-watcher",
		AdapterType:   "log_watcher",
	}
}

func unknownEvent(sessionFile string, rawBytes []byte, parseErr error, lineOffset int64) *audit.AuditEvent {
	return &audit.AuditEvent{
		ID:            deterministicID(sessionFile, lineOffset, 0),
		Timestamp:     time.Now().UTC(),
		AgentID:       "claude-code",
		ActionType:    audit.ActionUnknown,
		Action:        "unknown",
		Arguments:     fmt.Sprintf("parse_error: %v", parseErr),
		SessionID:     extractSessionIDFromFile(sessionFile),
		ParserVersion: ParserVersion,
		Outcome:       audit.OutcomeSuccess,
		AdapterID:     "log-watcher",
		AdapterType:   "log_watcher",
	}
}

func classifyTool(name string) audit.ActionType {
	switch name {
	case "Bash":
		return audit.ActionCommandExecution
	case "Read", "Write", "Edit", "Glob", "Grep":
		return audit.ActionFileAccess
	default:
		return audit.ActionToolCall
	}
}

func parseTimestamp(ts string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000Z", ts)
		if err != nil {
			return time.Now().UTC()
		}
	}
	return t
}

func extractSessionIDFromFile(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext == ".jsonl" {
		return strings.TrimSuffix(base, ext)
	}
	return base
}

// DriftRatio computes the ratio of unknown events to total events.
func DriftRatio(result *ParseResult) float64 {
	if result.Total == 0 {
		return 0
	}
	return float64(result.Unknown) / float64(result.Total)
}

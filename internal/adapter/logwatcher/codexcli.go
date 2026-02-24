package logwatcher

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

const CodexParserVersion = "codex-parser-v0.1"

var codexSessionIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var codexSessionModels sync.Map

type codexEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	ID          string `json:"id"`
	Timestamp   string `json:"timestamp"`
	CWD         string `json:"cwd"`
	CLI         string `json:"cli_version"`
	Model       string `json:"model"`
	ModelFamily string `json:"model_provider"`
}

type codexResponseItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Text      string          `json:"text"`
	Model     string          `json:"model"`
	Name      string          `json:"name"`
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Usage     *codexUsage     `json:"usage"`
}

type codexContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	ToolName  string          `json:"tool_name"`
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
}

type codexEventMsg struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

type codexTurnContext struct {
	CWD            string `json:"cwd"`
	Model          string `json:"model"`
	ApprovalPolicy string `json:"approval_policy"`
}

type codexTokenCount struct {
	Model        string                `json:"model"`
	InputTokens  int64                 `json:"input_tokens"`
	OutputTokens int64                 `json:"output_tokens"`
	Usage        *codexTokenCountUsage `json:"usage"`
}

type codexTokenCountUsage struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
}

type codexUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

func parseCodexLine(line []byte, sessionFile string, lineOffset int64) ([]*audit.AuditEvent, error) {
	if len(line) == 0 {
		return nil, nil
	}

	var env codexEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return []*audit.AuditEvent{codexUnknownEvent(sessionFile, line, err, lineOffset)}, nil
	}

	sessionID := extractCodexSessionID(sessionFile)
	timestamp := parseTimestamp(env.Timestamp)

	switch env.Type {
	case "session_meta":
		return parseCodexSessionMeta(env.Payload, sessionFile, sessionID, timestamp, lineOffset, line)
	case "response_item":
		return parseCodexResponseItem(env.Payload, sessionFile, sessionID, timestamp, lineOffset, line)
	case "token_count":
		return parseCodexTokenCount(env.Payload, sessionFile, sessionID, timestamp, lineOffset, line)
	case "turn_context":
		return parseCodexTurnContext(env.Payload, sessionFile, sessionID, timestamp, lineOffset, line)
	case "event_msg":
		return parseCodexEventMsg(env.Payload, sessionFile, sessionID, timestamp, lineOffset, line)
	default:
		return []*audit.AuditEvent{codexUnknownEvent(sessionFile, line, fmt.Errorf("unknown type: %s", env.Type), lineOffset)}, nil
	}
}

func parseCodexSessionMeta(payload json.RawMessage, sessionFile, fallbackSessionID string, timestamp time.Time, lineOffset int64, raw []byte) ([]*audit.AuditEvent, error) {
	var meta codexSessionMeta
	if err := json.Unmarshal(payload, &meta); err != nil {
		return []*audit.AuditEvent{codexUnknownEvent(sessionFile, raw, err, lineOffset)}, nil
	}

	sessionID := fallbackSessionID
	if meta.ID != "" {
		sessionID = meta.ID
	}
	if meta.Model != "" {
		setCodexSessionModel(sessionFile, sessionID, meta.Model)
	}

	args := map[string]string{}
	if meta.CLI != "" {
		args["cli_version"] = meta.CLI
	}
	if meta.ModelFamily != "" {
		args["model_provider"] = meta.ModelFamily
	}

	return []*audit.AuditEvent{codexBaseEvent(sessionFile, lineOffset, 0, raw, timestamp, sessionID, audit.ActionSystem, "session_meta", mustMarshalMap(args), meta.CWD, meta.Model)}, nil
}

func parseCodexResponseItem(payload json.RawMessage, sessionFile, sessionID string, timestamp time.Time, lineOffset int64, raw []byte) ([]*audit.AuditEvent, error) {
	var item codexResponseItem
	if err := json.Unmarshal(payload, &item); err != nil {
		return []*audit.AuditEvent{codexUnknownEvent(sessionFile, raw, err, lineOffset)}, nil
	}

	if item.Model == "" {
		item.Model = getCodexSessionModel(sessionFile, sessionID)
	}

	if isCodexIgnoredResponseType(item.Type) {
		return nil, nil
	}

	if item.Type == "message" {
		return parseCodexMessageItem(item, sessionFile, sessionID, timestamp, lineOffset, raw)
	}

	if evt, ok := codexToolEventFromResponseItem(item, sessionFile, sessionID, timestamp, lineOffset, raw); ok {
		return []*audit.AuditEvent{evt}, nil
	}

	return []*audit.AuditEvent{codexUnknownEvent(sessionFile, raw, fmt.Errorf("unknown response_item payload.type: %s", item.Type), lineOffset)}, nil
}

func codexToolEventFromResponseItem(item codexResponseItem, sessionFile, sessionID string, timestamp time.Time, lineOffset int64, raw []byte) (*audit.AuditEvent, bool) {
	name := strings.TrimSpace(firstNonEmpty(item.Name, item.ToolName))
	rawArgs := firstNonEmptyRaw(item.Arguments, item.Input)
	if !isCodexToolType(item.Type) && !looksLikeCodexToolResponseItem(item, rawArgs) {
		return nil, false
	}
	if name == "" {
		name = strings.TrimSpace(item.Type)
	}

	evt := codexBaseEvent(
		sessionFile,
		lineOffset,
		0,
		raw,
		timestamp,
		sessionID,
		audit.ActionToolCall,
		name,
		firstNonEmpty(normalizeRawJSON(rawArgs)),
		"",
		item.Model,
	)
	applyToolClassification(evt, "openai", name, rawArgs)
	if item.Usage != nil {
		evt.InputTokens = item.Usage.InputTokens
		evt.OutputTokens = item.Usage.OutputTokens
	}
	return evt, true
}

func parseCodexMessageItem(item codexResponseItem, sessionFile, sessionID string, timestamp time.Time, lineOffset int64, raw []byte) ([]*audit.AuditEvent, error) {
	if item.Role == "developer" {
		return nil, nil
	}

	var events []*audit.AuditEvent
	var userPrompts []string
	var assistantText []string
	toolIdx := 0

	blocks := parseCodexBlocks(item.Content)
	if len(blocks) == 0 && item.Text != "" {
		blocks = []codexContentBlock{{Type: "text", Text: item.Text}}
	}

	for _, block := range blocks {
		switch block.Type {
		case "input_text":
			if item.Role == "user" && strings.TrimSpace(block.Text) != "" {
				userPrompts = append(userPrompts, block.Text)
			}
		case "text", "output_text":
			if item.Role == "assistant" && strings.TrimSpace(block.Text) != "" {
				assistantText = append(assistantText, block.Text)
			}
		default:
			rawArgs := firstNonEmptyRaw(block.Arguments, block.Input)
			if isCodexToolType(block.Type) || looksLikeCodexToolContentBlock(block, rawArgs) {
				name := strings.TrimSpace(firstNonEmpty(block.Name, block.ToolName))
				if name == "" {
					name = strings.TrimSpace(block.Type)
				}
				args := firstNonEmpty(normalizeRawJSON(rawArgs))
				evt := codexBaseEvent(sessionFile, lineOffset, toolIdx, raw, timestamp, sessionID, audit.ActionToolCall, name, args, "", item.Model)
				applyToolClassification(evt, "openai", name, rawArgs)
				events = append(events, evt)
				toolIdx++
			}
		}
	}

	if item.Role == "user" {
		prompt := strings.TrimSpace(strings.Join(userPrompts, "\n"))
		if prompt == "" {
			prompt = strings.TrimSpace(item.Text)
		}
		if prompt != "" {
			events = append(events, codexBaseEvent(sessionFile, lineOffset, toolIdx, raw, timestamp, sessionID, audit.ActionAIRequest, "chat", prompt, "", item.Model))
		}
	}

	if item.Role == "assistant" && len(events) == 0 {
		responseText := strings.TrimSpace(strings.Join(assistantText, "\n"))
		events = append(events, codexBaseEvent(sessionFile, lineOffset, toolIdx, raw, timestamp, sessionID, audit.ActionAIRequest, "chat", responseText, "", item.Model))
	}

	if item.Usage != nil && len(events) > 0 {
		events[0].InputTokens = item.Usage.InputTokens
		events[0].OutputTokens = item.Usage.OutputTokens
		if events[0].Model == "" {
			events[0].Model = item.Model
		}
	}

	return events, nil
}

func parseCodexTokenCount(payload json.RawMessage, sessionFile, sessionID string, timestamp time.Time, lineOffset int64, raw []byte) ([]*audit.AuditEvent, error) {
	var tc codexTokenCount
	if err := json.Unmarshal(payload, &tc); err != nil {
		return []*audit.AuditEvent{codexUnknownEvent(sessionFile, raw, err, lineOffset)}, nil
	}

	input := tc.InputTokens
	output := tc.OutputTokens
	if tc.Usage != nil {
		if tc.Usage.InputTokens != nil {
			input = *tc.Usage.InputTokens
		}
		if tc.Usage.OutputTokens != nil {
			output = *tc.Usage.OutputTokens
		}
	}

	model := tc.Model
	if model == "" {
		model = getCodexSessionModel(sessionFile, sessionID)
	}
	e := codexBaseEvent(sessionFile, lineOffset, 0, raw, timestamp, sessionID, audit.ActionAIRequest, "token_count", "", "", model)
	e.InputTokens = input
	e.OutputTokens = output
	return []*audit.AuditEvent{e}, nil
}

func parseCodexTurnContext(payload json.RawMessage, sessionFile, sessionID string, timestamp time.Time, lineOffset int64, raw []byte) ([]*audit.AuditEvent, error) {
	var tc codexTurnContext
	if err := json.Unmarshal(payload, &tc); err != nil {
		return []*audit.AuditEvent{codexUnknownEvent(sessionFile, raw, err, lineOffset)}, nil
	}

	args := mustMarshalMap(map[string]string{"approval_policy": tc.ApprovalPolicy})
	if tc.Model != "" {
		setCodexSessionModel(sessionFile, sessionID, tc.Model)
	}
	e := codexBaseEvent(sessionFile, lineOffset, 0, raw, timestamp, sessionID, audit.ActionSystem, "turn_context", args, tc.CWD, tc.Model)
	return []*audit.AuditEvent{e}, nil
}

func parseCodexEventMsg(payload json.RawMessage, sessionFile, sessionID string, timestamp time.Time, lineOffset int64, raw []byte) ([]*audit.AuditEvent, error) {
	var msg codexEventMsg
	if err := json.Unmarshal(payload, &msg); err != nil {
		return []*audit.AuditEvent{codexUnknownEvent(sessionFile, raw, err, lineOffset)}, nil
	}

	if msg.Type == "turn_aborted" {
		e := codexBaseEvent(sessionFile, lineOffset, 0, raw, timestamp, sessionID, audit.ActionSystem, "turn_aborted", firstNonEmpty(msg.Reason, msg.Message), "", "")
		e.Outcome = audit.OutcomeFailure
		return []*audit.AuditEvent{e}, nil
	}

	return nil, nil
}

func codexUnknownEvent(sessionFile string, raw []byte, parseErr error, lineOffset int64) *audit.AuditEvent {
	e := codexBaseEvent(sessionFile, lineOffset, 0, raw, time.Now().UTC(), extractCodexSessionID(sessionFile), audit.ActionUnknown, "unknown", fmt.Sprintf("parse_error: %v", parseErr), "", "")
	e.Outcome = audit.OutcomeFailure
	return e
}

func codexBaseEvent(sessionFile string, lineOffset int64, suffix int, raw []byte, ts time.Time, sessionID string, actionType audit.ActionType, action, args, cwd, model string) *audit.AuditEvent {
	return &audit.AuditEvent{
		ID:            deterministicID(sessionFile, lineOffset, suffix, raw),
		Timestamp:     ts,
		AgentID:       "codex-cli",
		ActionType:    actionType,
		Action:        action,
		Arguments:     args,
		SessionID:     sessionID,
		WorkingDir:    cwd,
		ParserVersion: CodexParserVersion,
		Outcome:       audit.OutcomeSuccess,
		Model:         model,
		AdapterID:     "log-watcher",
		AdapterType:   "log_watcher",
	}
}

func parseCodexBlocks(content json.RawMessage) []codexContentBlock {
	if len(content) == 0 || string(content) == "null" {
		return nil
	}

	var blocks []codexContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		return blocks
	}

	var single codexContentBlock
	if err := json.Unmarshal(content, &single); err == nil && single.Type != "" {
		return []codexContentBlock{single}
	}

	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return []codexContentBlock{{Type: "text", Text: text}}
	}

	return nil
}

func normalizeRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	return string(raw)
}

func extractCodexSessionID(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if match := codexSessionIDPattern.FindString(base); match != "" {
		return strings.ToLower(match)
	}
	return base
}

func isCodexToolType(t string) bool {
	switch strings.ToLower(t) {
	case "tool_call", "function_call", "tool_use":
		return true
	default:
		return false
	}
}

func isCodexIgnoredResponseType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "reasoning", "function_call_output":
		return true
	default:
		return false
	}
}

func looksLikeCodexToolResponseItem(item codexResponseItem, rawArgs json.RawMessage) bool {
	t := strings.ToLower(strings.TrimSpace(item.Type))
	if t == "" || t == "message" || isCodexIgnoredResponseType(t) {
		return false
	}
	if len(rawArgs) > 0 && string(rawArgs) != "null" {
		return true
	}
	if strings.TrimSpace(item.Name) != "" || strings.TrimSpace(item.ToolName) != "" {
		return true
	}
	return strings.HasSuffix(t, "_call") || strings.Contains(t, "tool") || strings.Contains(t, "search") || strings.Contains(t, "exec")
}

func looksLikeCodexToolContentBlock(block codexContentBlock, rawArgs json.RawMessage) bool {
	t := strings.ToLower(strings.TrimSpace(block.Type))
	if t == "" || t == "text" || t == "output_text" || t == "input_text" {
		return false
	}
	if len(rawArgs) > 0 && string(rawArgs) != "null" {
		return true
	}
	if strings.TrimSpace(block.Name) != "" || strings.TrimSpace(block.ToolName) != "" {
		return true
	}
	return strings.HasSuffix(t, "_call") || strings.Contains(t, "tool") || strings.Contains(t, "search") || strings.Contains(t, "exec")
}

func codexSessionKey(sessionFile, sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	return sessionFile
}

func setCodexSessionModel(sessionFile, sessionID, model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	codexSessionModels.Store(codexSessionKey(sessionFile, sessionID), model)
}

func getCodexSessionModel(sessionFile, sessionID string) string {
	v, ok := codexSessionModels.Load(codexSessionKey(sessionFile, sessionID))
	if !ok {
		return ""
	}
	model, _ := v.(string)
	return model
}

func mustMarshalMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}

	clean := make(map[string]string)
	for k, v := range m {
		if strings.TrimSpace(v) == "" {
			continue
		}
		clean[k] = v
	}
	if len(clean) == 0 {
		return ""
	}

	b, err := json.Marshal(clean)
	if err != nil {
		return ""
	}
	return string(b)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyRaw(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		return value
	}
	return nil
}

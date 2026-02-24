package logwatcher

import (
	"os"
	"strings"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/classify"
)

func TestParseLineForSource_CodexByPath(t *testing.T) {
	line := `{"timestamp":"2026-02-24T10:00:00.000Z","type":"session_meta","payload":{"id":"019c7b92-c543-7ac3-aad5-e8681852a8c5","cwd":"/home/user/project","cli_version":"0.98.0","model_provider":"openai"}}`

	events, err := ParseLineForSource([]byte(line), "/home/user/.codex/sessions/2026/02/24/rollout-2026-02-24T10-00-00-019c7b92-c543-7ac3-aad5-e8681852a8c5.jsonl", 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.AgentID != "codex-cli" {
		t.Fatalf("expected codex-cli, got %s", e.AgentID)
	}
	if e.ActionType != audit.ActionSystem {
		t.Fatalf("expected system action, got %s", e.ActionType)
	}
	if e.Action != "session_meta" {
		t.Fatalf("expected session_meta action, got %s", e.Action)
	}
}

func TestParseLineForSource_CodexByType(t *testing.T) {
	line := `{"timestamp":"2026-02-24T10:00:04.000Z","type":"token_count","payload":{"model":"gpt-5.1-codex-mini","input_tokens":130,"output_tokens":20}}`

	events, err := ParseLineForSource([]byte(line), "/tmp/session.jsonl", 10)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.AgentID != "codex-cli" {
		t.Fatalf("expected codex-cli, got %s", e.AgentID)
	}
	if e.InputTokens != 130 || e.OutputTokens != 20 {
		t.Fatalf("expected 130/20 tokens, got %d/%d", e.InputTokens, e.OutputTokens)
	}
}

func TestParseCodexSessionMeta_DoesNotUseProviderAsModel(t *testing.T) {
	line := `{"timestamp":"2026-02-24T10:00:00.000Z","type":"session_meta","payload":{"id":"019c7b92-c543-7ac3-aad5-e8681852a8c5","model_provider":"openai"}}`

	events, err := parseCodexLine([]byte(line), "/tmp/rollout-2026-02-24T10-00-00-019c7b92-c543-7ac3-aad5-e8681852a8c5.jsonl", 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Model != "" {
		t.Fatalf("expected empty model when only provider is present, got %q", events[0].Model)
	}
}

func TestParseCodexTokenCount_PartialUsageOverridesIndependently(t *testing.T) {
	line := `{"timestamp":"2026-02-24T10:00:04.000Z","type":"token_count","payload":{"model":"gpt-5.1-codex-mini","input_tokens":130,"output_tokens":20,"usage":{"output_tokens":30}}}`

	events, err := parseCodexLine([]byte(line), "/tmp/session.jsonl", 10)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.InputTokens != 130 || e.OutputTokens != 30 {
		t.Fatalf("expected 130/30 tokens, got %d/%d", e.InputTokens, e.OutputTokens)
	}
}

func TestParseCodexResponseItem_UserPrompt(t *testing.T) {
	line := `{"timestamp":"2026-02-24T10:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Read the main.go file"}]}}`

	events, err := parseCodexLine([]byte(line), "/tmp/rollout-2026-02-24T10-00-00-019c7b92-c543-7ac3-aad5-e8681852a8c5.jsonl", 1)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ActionType != audit.ActionAIRequest {
		t.Fatalf("expected ai_request, got %s", events[0].ActionType)
	}
	if !strings.Contains(events[0].Arguments, "Read the main.go file") {
		t.Fatalf("expected prompt arguments, got %q", events[0].Arguments)
	}
}

func TestParseCodexResponseItem_ToolCallClassification(t *testing.T) {
	line := `{"timestamp":"2026-02-24T11:00:02.000Z","type":"response_item","payload":{"type":"message","role":"assistant","model":"gpt-5.1-codex","content":[{"type":"tool_call","name":"Bash","arguments":{"command":"go test ./..."}}],"usage":{"input_tokens":200,"output_tokens":15}}}`

	events, err := parseCodexLine([]byte(line), "/tmp/rollout-2026-02-24T11-00-00-019c7b9d-932d-7bb3-ae9b-e8e13b639117.jsonl", 2)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.ActionType != audit.ActionCommandExecution {
		t.Fatalf("expected command_execution, got %s", e.ActionType)
	}
	if e.Provider != "openai" {
		t.Fatalf("expected openai provider, got %s", e.Provider)
	}
	if e.ToolCategory != classify.CategoryShell || e.ToolEffect != classify.EffectExecute {
		t.Fatalf("expected shell/execute taxonomy, got %s/%s", e.ToolCategory, e.ToolEffect)
	}
	if e.ClassificationSource != classify.SourceExact {
		t.Fatalf("expected exact classification source, got %s", e.ClassificationSource)
	}
	if e.InputTokens != 200 || e.OutputTokens != 15 {
		t.Fatalf("expected usage 200/15, got %d/%d", e.InputTokens, e.OutputTokens)
	}
}

func TestParseCodexResponseItem_FunctionCallExecCommandUsesCommandExecution(t *testing.T) {
	sessionPath := "/tmp/rollout-2026-02-24T11-00-00-019c7b9d-932d-7bb3-ae9b-e8e13b639117.jsonl"
	contextLine := `{"timestamp":"2026-02-24T11:00:01.000Z","type":"turn_context","payload":{"cwd":"/tmp","model":"gpt-5.1-codex-mini","approval_policy":"on-request"}}`
	toolLine := `{"timestamp":"2026-02-24T11:00:02.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"git status -sb\"}"}}`

	if _, err := parseCodexLine([]byte(contextLine), sessionPath, 1); err != nil {
		t.Fatalf("parse turn_context: %v", err)
	}

	events, err := parseCodexLine([]byte(toolLine), sessionPath, 2)
	if err != nil {
		t.Fatalf("parse function_call: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.ActionType != audit.ActionCommandExecution {
		t.Fatalf("expected command_execution, got %s", e.ActionType)
	}
	if e.ToolCategory != classify.CategoryShell || e.ClassificationSource != classify.SourceExact {
		t.Fatalf("expected exact shell classification, got %s/%s", e.ToolCategory, e.ClassificationSource)
	}
	if e.Model != "gpt-5.1-codex-mini" {
		t.Fatalf("expected model propagated from turn_context, got %q", e.Model)
	}
}

func TestParseCodexResponseItem_ReasoningIgnored(t *testing.T) {
	line := `{"timestamp":"2026-02-24T11:00:02.000Z","type":"response_item","payload":{"type":"reasoning","summary":[]}}`

	events, err := parseCodexLine([]byte(line), "/tmp/rollout-2026-02-24T11-00-00-019c7b9d-932d-7bb3-ae9b-e8e13b639117.jsonl", 2)
	if err != nil {
		t.Fatalf("parse reasoning: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events for reasoning, got %d", len(events))
	}
}

func TestParseCodexResponseItem_WebSearchCallClassifiedAsNetwork(t *testing.T) {
	line := `{"timestamp":"2026-02-24T18:21:41.000Z","type":"response_item","payload":{"type":"web_search_call","arguments":{"query":"weather in toronto"}}}`

	events, err := parseCodexLine([]byte(line), "/tmp/rollout-2026-02-24T18-00-00-019c7b9d-932d-7bb3-ae9b-e8e13b639117.jsonl", 12)
	if err != nil {
		t.Fatalf("parse web_search_call: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.ActionType != audit.ActionToolCall {
		t.Fatalf("expected tool_call action type for network tools, got %s", e.ActionType)
	}
	if e.Action != "web_search_call" {
		t.Fatalf("expected fallback tool name from type, got %q", e.Action)
	}
	if e.ToolCategory != classify.CategoryNetwork || e.ToolEffect != classify.EffectExecute {
		t.Fatalf("expected network/execute taxonomy, got %s/%s", e.ToolCategory, e.ToolEffect)
	}
	if e.ClassificationSource != classify.SourcePattern {
		t.Fatalf("expected pattern classification source, got %s", e.ClassificationSource)
	}
}

func TestParseCodexResponseItem_ExecutionResultIgnored(t *testing.T) {
	line := `{"timestamp":"2026-02-24T18:21:45.000Z","type":"response_item","payload":{"type":"execution_result","arguments":{"status":"ok","duration_ms":12}}}`

	events, err := parseCodexLine([]byte(line), "/tmp/rollout-2026-02-24T18-00-00-019c7b9d-932d-7bb3-ae9b-e8e13b639117.jsonl", 15)
	if err != nil {
		t.Fatalf("parse execution_result: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events for execution_result, got %d", len(events))
	}
}

func TestParseCodexResponseItem_CodeSearchResultIgnored(t *testing.T) {
	line := `{"timestamp":"2026-02-24T18:21:46.000Z","type":"response_item","payload":{"type":"code_search_result","arguments":{"count":3}}}`

	events, err := parseCodexLine([]byte(line), "/tmp/rollout-2026-02-24T18-00-00-019c7b9d-932d-7bb3-ae9b-e8e13b639117.jsonl", 16)
	if err != nil {
		t.Fatalf("parse code_search_result: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events for code_search_result, got %d", len(events))
	}
}

func TestParseCodexFixture_Basic(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/codex-cli/session-basic.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var events []*audit.AuditEvent
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parsed, err := ParseLineForSource([]byte(line), "/home/user/.codex/sessions/2026/02/24/rollout-2026-02-24T10-00-00-019c7b92-c543-7ac3-aad5-e8681852a8c5.jsonl", 0)
		if err != nil {
			t.Fatalf("parse fixture line: %v", err)
		}
		events = append(events, parsed...)
	}

	if len(events) == 0 {
		t.Fatal("expected parsed events")
	}

	var hasPrompt, hasFileAccess, hasTokenCount bool
	for _, e := range events {
		if e.ActionType == audit.ActionAIRequest && e.Action == "chat" {
			hasPrompt = true
		}
		if e.ActionType == audit.ActionFileAccess {
			hasFileAccess = true
		}
		if e.ActionType == audit.ActionAIRequest && e.Action == "token_count" {
			hasTokenCount = true
		}
	}

	if !hasPrompt {
		t.Fatal("expected chat ai_request from user prompt")
	}
	if !hasFileAccess {
		t.Fatal("expected file_access from tool_call")
	}
	if !hasTokenCount {
		t.Fatal("expected token_count event")
	}
}

func TestParseCodexFixture_Malformed(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/codex-cli/session-malformed.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	unknownCount := 0
	hasTurnAborted := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parsed, err := ParseLineForSource([]byte(line), "/home/user/.codex/sessions/2026/02/24/rollout-2026-02-24T12-00-00-019c7b9d-932d-7bb3-ae9b-e8e13b639117.jsonl", 0)
		if err != nil {
			t.Fatalf("parse fixture line: %v", err)
		}
		for _, e := range parsed {
			if e.ActionType == audit.ActionUnknown {
				unknownCount++
			}
			if e.Action == "turn_aborted" {
				hasTurnAborted = true
			}
		}
	}

	if unknownCount == 0 {
		t.Fatal("expected unknown events for malformed fixture")
	}
	if !hasTurnAborted {
		t.Fatal("expected turn_aborted system event")
	}
}

func TestExtractCodexSessionID(t *testing.T) {
	got := extractCodexSessionID("/home/user/.codex/sessions/2026/02/24/rollout-2026-02-24T10-00-00-019c7b92-c543-7ac3-aad5-e8681852a8c5.jsonl")
	if got != "019c7b92-c543-7ac3-aad5-e8681852a8c5" {
		t.Fatalf("expected UUID session id, got %s", got)
	}
}

package logwatcher

import (
	"os"
	"strings"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestParseLine_ToolUse(t *testing.T) {
	line := `{"type":"assistant","sessionId":"sess-001","cwd":"/home/user","message":{"model":"claude-sonnet-4-5-20250929","role":"assistant","content":[{"type":"tool_use","id":"toolu_001","name":"Read","input":{"file_path":"/tmp/test.go"}}],"usage":{"input_tokens":100,"output_tokens":10}},"uuid":"uuid-001","timestamp":"2026-02-22T14:00:00.000Z"}`

	events, err := ParseLine([]byte(line), "/tmp/session.jsonl")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.ActionType != audit.ActionFileAccess {
		t.Fatalf("expected file_access, got %s", e.ActionType)
	}
	if e.Action != "Read" {
		t.Fatalf("expected Read, got %s", e.Action)
	}
	if e.Model != "claude-sonnet-4-5-20250929" {
		t.Fatalf("expected model, got %s", e.Model)
	}
	if e.InputTokens != 100 {
		t.Fatalf("expected 100 input tokens, got %d", e.InputTokens)
	}
}

func TestParseLine_BashCommand(t *testing.T) {
	line := `{"type":"assistant","sessionId":"sess-001","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_002","name":"Bash","input":{"command":"go test ./..."}}],"usage":{"input_tokens":50,"output_tokens":20}},"uuid":"uuid-002","timestamp":"2026-02-22T14:00:00.000Z"}`

	events, _ := ParseLine([]byte(line), "/tmp/session.jsonl")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ActionType != audit.ActionCommandExecution {
		t.Fatalf("expected command_execution, got %s", events[0].ActionType)
	}
}

func TestParseLine_AIRequest(t *testing.T) {
	line := `{"type":"assistant","sessionId":"sess-001","message":{"model":"claude-sonnet-4-5-20250929","role":"assistant","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":500,"output_tokens":100}},"uuid":"uuid-003","timestamp":"2026-02-22T14:00:00.000Z"}`

	events, _ := ParseLine([]byte(line), "/tmp/session.jsonl")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ActionType != audit.ActionAIRequest {
		t.Fatalf("expected ai_request, got %s", events[0].ActionType)
	}
}

func TestParseLine_SkipTypes(t *testing.T) {
	skips := []string{
		`{"type":"queue-operation","operation":"dequeue","timestamp":"2026-02-22T14:00:00.000Z"}`,
		`{"type":"file-history-snapshot","messageId":"m1"}`,
		`{"type":"summary","summary":"test"}`,
	}

	for _, line := range skips {
		events, err := ParseLine([]byte(line), "/tmp/session.jsonl")
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", line[:30], err)
		}
		if len(events) != 0 {
			t.Fatalf("expected skip for %s, got %d events", line[:30], len(events))
		}
	}
}

func TestParseLine_MalformedJSON(t *testing.T) {
	events, err := ParseLine([]byte("not valid json"), "/tmp/session.jsonl")
	if err != nil {
		t.Fatalf("should not return error, got %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 unknown event, got %d", len(events))
	}
	if events[0].ActionType != audit.ActionUnknown {
		t.Fatalf("expected unknown, got %s", events[0].ActionType)
	}
}

func TestParseLine_UnknownType(t *testing.T) {
	line := `{"type":"future_record_type","newField":"value","timestamp":"2026-02-22T14:00:00.000Z"}`
	events, _ := ParseLine([]byte(line), "/tmp/session.jsonl")
	if len(events) != 1 {
		t.Fatalf("expected 1 unknown event, got %d", len(events))
	}
	if events[0].ActionType != audit.ActionUnknown {
		t.Fatalf("expected unknown, got %s", events[0].ActionType)
	}
}

func TestParseLine_EmptyLine(t *testing.T) {
	events, err := ParseLine([]byte(""), "/tmp/session.jsonl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events != nil {
		t.Fatal("expected nil for empty line")
	}
}

func TestParseLine_UserToolResult_Skipped(t *testing.T) {
	line := `{"type":"user","sessionId":"sess-001","message":{"role":"user","content":[{"tool_use_id":"toolu_001","type":"tool_result","content":"ok"}]},"uuid":"uuid-r01","timestamp":"2026-02-22T14:00:00.000Z","toolUseResult":"success"}`

	events, _ := ParseLine([]byte(line), "/tmp/session.jsonl")
	if len(events) != 0 {
		t.Fatalf("expected tool result to be skipped, got %d events", len(events))
	}
}

func TestParseFixture_Basic(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/claude-code/session-basic.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var result ParseResult
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result.Total++
		events, _ := ParseLine([]byte(line), "session-basic.jsonl")
		if events == nil {
			result.Skipped++
		} else {
			for _, e := range events {
				if e.ActionType == audit.ActionUnknown {
					result.Unknown++
				}
			}
			result.Events = append(result.Events, events...)
		}
	}

	if len(result.Events) == 0 {
		t.Fatal("expected events from basic fixture")
	}

	// Should have tool calls and AI requests
	var hasToolCall, hasAIReq bool
	for _, e := range result.Events {
		if e.ActionType == audit.ActionFileAccess {
			hasToolCall = true
		}
		if e.ActionType == audit.ActionAIRequest {
			hasAIReq = true
		}
	}
	if !hasToolCall {
		t.Fatal("expected at least one tool call event")
	}
	if !hasAIReq {
		t.Fatal("expected at least one AI request event")
	}
}

func TestParseFixture_Malformed_NoPanic(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/claude-code/session-malformed.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var result ParseResult
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result.Total++
		events, _ := ParseLine([]byte(line), "session-malformed.jsonl")
		if events != nil {
			for _, e := range events {
				if e.ActionType == audit.ActionUnknown {
					result.Unknown++
				}
			}
			result.Events = append(result.Events, events...)
		}
	}

	// Malformed fixture should have unknown events but no panics
	if result.Unknown == 0 {
		t.Fatal("expected unknown events from malformed fixture")
	}
}

func TestParseFixture_Empty(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/claude-code/session-empty.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var total int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		total++
		ParseLine([]byte(line), "session-empty.jsonl") // should not panic
	}
}

func TestDriftRatio(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		unknown  int
		expected float64
	}{
		{"no events", 0, 0, 0},
		{"no unknowns", 10, 0, 0},
		{"half unknown", 10, 5, 0.5},
		{"all unknown", 10, 10, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ParseResult{Total: tt.total, Unknown: tt.unknown}
			got := DriftRatio(r)
			if got != tt.expected {
				t.Fatalf("expected %f, got %f", tt.expected, got)
			}
		})
	}
}

func TestClassifyTool(t *testing.T) {
	tests := []struct {
		name   string
		expect audit.ActionType
	}{
		{"Bash", audit.ActionCommandExecution},
		{"Read", audit.ActionFileAccess},
		{"Write", audit.ActionFileAccess},
		{"Edit", audit.ActionFileAccess},
		{"Glob", audit.ActionFileAccess},
		{"Grep", audit.ActionFileAccess},
		{"Task", audit.ActionToolCall},
		{"WebSearch", audit.ActionToolCall},
	}

	for _, tt := range tests {
		got := classifyTool(tt.name)
		if got != tt.expect {
			t.Fatalf("classifyTool(%s): expected %s, got %s", tt.name, tt.expect, got)
		}
	}
}

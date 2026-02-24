package logwatcher

import (
	"testing"

	"github.com/crabwise-ai/crabwise/internal/classify"
)

func TestToolClassificationConformance_ClaudeAndCodex(t *testing.T) {
	tests := []struct {
		name           string
		claudeLine     string
		codexLine      string
		expectCategory string
		expectEffect   string
	}{
		{
			name:           "bash",
			claudeLine:     `{"type":"assistant","sessionId":"sess-001","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./..."}}],"usage":{"input_tokens":1,"output_tokens":1}},"uuid":"u1","timestamp":"2026-02-22T14:00:00.000Z"}`,
			codexLine:      `{"timestamp":"2026-02-24T11:00:02.000Z","type":"response_item","payload":{"type":"message","role":"assistant","model":"gpt-5.1-codex","content":[{"type":"tool_call","name":"Bash","arguments":{"command":"go test ./..."}}],"usage":{"input_tokens":1,"output_tokens":1}}}`,
			expectCategory: classify.CategoryShell,
			expectEffect:   classify.EffectExecute,
		},
		{
			name:           "read",
			claudeLine:     `{"type":"assistant","sessionId":"sess-001","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Read","input":{"file_path":"/tmp/main.go"}}],"usage":{"input_tokens":1,"output_tokens":1}},"uuid":"u2","timestamp":"2026-02-22T14:00:00.000Z"}`,
			codexLine:      `{"timestamp":"2026-02-24T10:00:02.000Z","type":"response_item","payload":{"type":"message","role":"assistant","model":"gpt-5.1-codex-mini","content":[{"type":"tool_call","name":"Read","arguments":{"file_path":"/tmp/main.go"}}],"usage":{"input_tokens":1,"output_tokens":1}}}`,
			expectCategory: classify.CategoryFileRead,
			expectEffect:   classify.EffectReadOnly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claudeEvents, err := ParseLine([]byte(tt.claudeLine), "/tmp/claude.jsonl", 0)
			if err != nil {
				t.Fatalf("parse claude line: %v", err)
			}
			if len(claudeEvents) != 1 {
				t.Fatalf("expected one claude event, got %d", len(claudeEvents))
			}

			codexEvents, err := ParseLineForSource([]byte(tt.codexLine), "/home/user/.codex/sessions/2026/02/24/rollout-2026-02-24T10-00-00-019c7b92-c543-7ac3-aad5-e8681852a8c5.jsonl", 0)
			if err != nil {
				t.Fatalf("parse codex line: %v", err)
			}
			if len(codexEvents) != 1 {
				t.Fatalf("expected one codex event, got %d", len(codexEvents))
			}

			claude := claudeEvents[0]
			codex := codexEvents[0]

			if claude.ToolCategory != tt.expectCategory || codex.ToolCategory != tt.expectCategory {
				t.Fatalf("expected category %s, got claude=%s codex=%s", tt.expectCategory, claude.ToolCategory, codex.ToolCategory)
			}
			if claude.ToolEffect != tt.expectEffect || codex.ToolEffect != tt.expectEffect {
				t.Fatalf("expected effect %s, got claude=%s codex=%s", tt.expectEffect, claude.ToolEffect, codex.ToolEffect)
			}
			if claude.ClassificationSource != classify.SourceExact || codex.ClassificationSource != classify.SourceExact {
				t.Fatalf("expected exact source, got claude=%s codex=%s", claude.ClassificationSource, codex.ClassificationSource)
			}
		})
	}
}

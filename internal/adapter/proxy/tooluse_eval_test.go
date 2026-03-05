package proxy

import (
	"encoding/json"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/classify"
)

type stubEvaluator struct {
	blockTool string // if set, block any tool_use with this ToolName
}

type noopClassifier struct{}

func (n *noopClassifier) Classify(provider, toolName string, argKeys []string) classify.ClassifyResult {
	return classify.ClassifyResult{}
}

func (n *noopClassifier) UnclassifiedCount() uint64 { return 0 }

func (s *stubEvaluator) Evaluate(e *audit.AuditEvent) audit.EvalResult {
	if s.blockTool != "" && e.ToolName == s.blockTool {
		return audit.EvalResult{
			Evaluated: []string{"test-commandment"},
			Triggered: []audit.TriggeredRule{{
				Name:        "test-commandment",
				Enforcement: "block",
				Message:     "blocked by test",
			}},
		}
	}
	return audit.EvalResult{}
}

func TestEvaluateToolUseBlocks_Blocked(t *testing.T) {
	p := &Proxy{evaluator: &stubEvaluator{blockTool: "Bash"}, classifier: &noopClassifier{}}
	blocks := []ToolUseBlock{{
		ID:        "call_1",
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`{"command":"rm -rf /"}`),
		Targets:   ParseTargets("Bash", json.RawMessage(`{"command":"rm -rf /"}`)),
	}}
	blocked, commandmentID := p.evaluateToolUseBlocks(blocks, "openai", "gpt-4o")
	if !blocked {
		t.Fatal("expected blocked=true")
	}
	if commandmentID == "" {
		t.Fatal("expected commandment_id to be set")
	}
}

func TestEvaluateToolUseBlocks_NotBlocked(t *testing.T) {
	p := &Proxy{evaluator: &stubEvaluator{}, classifier: &noopClassifier{}}
	blocks := []ToolUseBlock{{
		ID:        "call_1",
		ToolName:  "Read",
		ToolInput: json.RawMessage(`{"file_path":"/src/main.go"}`),
	}}
	blocked, _ := p.evaluateToolUseBlocks(blocks, "openai", "gpt-4o")
	if blocked {
		t.Fatal("expected blocked=false")
	}
}

func TestEvaluateToolUseBlocks_Empty(t *testing.T) {
	p := &Proxy{evaluator: &stubEvaluator{blockTool: "Bash"}, classifier: &noopClassifier{}}
	blocked, _ := p.evaluateToolUseBlocks(nil, "openai", "gpt-4o")
	if blocked {
		t.Fatal("expected blocked=false for empty blocks")
	}
}

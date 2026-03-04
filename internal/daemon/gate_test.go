package daemon

import (
	"encoding/json"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

type stubCommandments struct{}

func (s *stubCommandments) Evaluate(e *audit.AuditEvent) audit.EvalResult {
	if e.ToolName == "Bash" {
		return audit.EvalResult{
			Triggered: []audit.TriggeredRule{{Name: "no-bash", Enforcement: "block", Message: "bash blocked"}},
		}
	}
	return audit.EvalResult{}
}
func (s *stubCommandments) Redact(*audit.AuditEvent, bool) {}
func (s *stubCommandments) List() []CommandmentRuleSummary  { return nil }
func (s *stubCommandments) Test(e *audit.AuditEvent) audit.EvalResult { return s.Evaluate(e) }
func (s *stubCommandments) Reload() (int, error)            { return 0, nil }

func TestGateEvaluateHandler_Block(t *testing.T) {
	handler := makeGateEvaluateHandler(&stubCommandments{})
	params, _ := json.Marshal(GateEvaluateParams{
		AgentID:      "claude-code",
		ToolName:     "Bash",
		ToolCategory: "shell",
		ToolEffect:   "write",
	})
	result, err := handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := result.(GateEvaluateResult)
	if res.Decision != "block" {
		t.Fatalf("expected decision block, got %q", res.Decision)
	}
	if res.CommandmentID != "no-bash" {
		t.Errorf("expected commandment_id no-bash, got %q", res.CommandmentID)
	}
	if res.GateEventID == "" {
		t.Error("expected gate_event_id to be set")
	}
}

func TestGateEvaluateHandler_Pass(t *testing.T) {
	handler := makeGateEvaluateHandler(&stubCommandments{})
	params, _ := json.Marshal(GateEvaluateParams{
		AgentID:  "claude-code",
		ToolName: "Read",
	})
	result, err := handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := result.(GateEvaluateResult)
	if res.Decision != "pass" {
		t.Fatalf("expected decision pass, got %q", res.Decision)
	}
}

func TestGateEvaluateHandler_TargetsMappedToArguments(t *testing.T) {
	var capturedEvent *audit.AuditEvent
	svc := &capturingCommandments{fn: func(e *audit.AuditEvent) audit.EvalResult {
		capturedEvent = e
		return audit.EvalResult{}
	}}
	handler := makeGateEvaluateHandler(svc)
	params, _ := json.Marshal(GateEvaluateParams{
		AgentID:  "claude-code",
		ToolName: "Bash",
		Targets: GateTargets{
			Argv:     []string{"rm", "-rf", "/tmp/x"},
			Paths:    []string{"/tmp/x"},
			PathMode: "delete",
		},
	})
	_, err := handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedEvent == nil {
		t.Fatal("expected Evaluate to be called")
	}
	if capturedEvent.Arguments == "" {
		t.Fatal("expected Arguments to be populated from targets")
	}
}

type capturingCommandments struct {
	fn func(*audit.AuditEvent) audit.EvalResult
}

func (c *capturingCommandments) Evaluate(e *audit.AuditEvent) audit.EvalResult { return c.fn(e) }
func (c *capturingCommandments) Redact(*audit.AuditEvent, bool)                {}
func (c *capturingCommandments) List() []CommandmentRuleSummary                { return nil }
func (c *capturingCommandments) Test(e *audit.AuditEvent) audit.EvalResult     { return c.fn(e) }
func (c *capturingCommandments) Reload() (int, error)                          { return 0, nil }

package commandments

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestEvaluate_OrderPriorityDescThenNameAsc(t *testing.T) {
	rules := []Commandment{
		{
			Name:        "bravo",
			Enforcement: "warn",
			Priority:    10,
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"action_type": {Type: MatcherTypeExact, Pattern: "command_execution"},
			},
		},
		{
			Name:        "alpha",
			Enforcement: "warn",
			Priority:    10,
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"action_type": {Type: MatcherTypeExact, Pattern: "command_execution"},
			},
		},
		{
			Name:        "zed",
			Enforcement: "warn",
			Priority:    20,
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"action_type": {Type: MatcherTypeExact, Pattern: "command_execution"},
			},
		},
		{
			Name:        "disabled-first",
			Enforcement: "warn",
			Priority:    100,
			Enabled:     boolPtr(false),
			Match: map[string]MatchCondition{
				"action_type": {Type: MatcherTypeExact, Pattern: "command_execution"},
			},
		},
	}

	compiled, err := compileRules(rules)
	if err != nil {
		t.Fatalf("compile rules: %v", err)
	}

	engine := &Engine{rules: compiled}
	evt := &audit.AuditEvent{ActionType: audit.ActionCommandExecution, Arguments: "git push origin main"}
	result := engine.Evaluate(evt)

	expected := []string{"zed", "alpha", "bravo"}
	if !equalStrings(result.Evaluated, expected) {
		t.Fatalf("unexpected evaluated order: got=%v want=%v", result.Evaluated, expected)
	}

	if len(result.Triggered) != len(expected) {
		t.Fatalf("unexpected triggered len: got=%d want=%d", len(result.Triggered), len(expected))
	}
	for i, name := range expected {
		if result.Triggered[i].Name != name {
			t.Fatalf("unexpected triggered order: got=%v", result.Triggered)
		}
	}
}

func TestEvaluate_FullArgumentsStringOnly(t *testing.T) {
	rules := []Commandment{
		{
			Name:        "path-only-no-match",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"arguments": {Type: MatcherTypeExact, Pattern: "/tmp/.env"},
			},
		},
		{
			Name:        "full-json-match",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"arguments": {Type: MatcherTypeExact, Pattern: `{"file_path":"/tmp/.env"}`},
			},
		},
	}

	compiled, err := compileRules(rules)
	if err != nil {
		t.Fatalf("compile rules: %v", err)
	}
	engine := &Engine{rules: compiled}

	evt := &audit.AuditEvent{Arguments: `{"file_path":"/tmp/.env"}`}
	result := engine.Evaluate(evt)

	if len(result.Triggered) != 1 || result.Triggered[0].Name != "full-json-match" {
		t.Fatalf("unexpected trigger result: %+v", result.Triggered)
	}
}

func TestEvaluate_NumericNonNumericNoMatch(t *testing.T) {
	rules := []Commandment{
		{
			Name:        "numeric-arguments",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"arguments": {
					Type:  MatcherTypeNumeric,
					Op:    "gte",
					Value: 10,
				},
			},
		},
	}

	compiled, err := compileRules(rules)
	if err != nil {
		t.Fatalf("compile rules: %v", err)
	}
	engine := &Engine{rules: compiled}

	result := engine.Evaluate(&audit.AuditEvent{Arguments: "not-a-number"})
	if len(result.Triggered) != 0 {
		t.Fatalf("expected non-numeric numeric comparison to no-match, got %+v", result.Triggered)
	}
}

func TestEvaluate_NumericZeroValueFieldsMatch(t *testing.T) {
	rules := []Commandment{
		{
			Name:        "zero-input-tokens",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"input_tokens": {
					Type:  MatcherTypeNumeric,
					Op:    "eq",
					Value: 0,
				},
			},
		},
		{
			Name:        "zero-output-tokens",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"output_tokens": {
					Type:  MatcherTypeNumeric,
					Op:    "eq",
					Value: 0,
				},
			},
		},
		{
			Name:        "zero-cost",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"cost_usd": {
					Type:  MatcherTypeNumeric,
					Op:    "eq",
					Value: 0,
				},
			},
		},
		{
			Name:        "zero-agent-pid",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"agent_pid": {
					Type:  MatcherTypeNumeric,
					Op:    "eq",
					Value: 0,
				},
			},
		},
	}

	compiled, err := compileRules(rules)
	if err != nil {
		t.Fatalf("compile rules: %v", err)
	}
	engine := &Engine{rules: compiled}

	result := engine.Evaluate(&audit.AuditEvent{
		InputTokens:  0,
		OutputTokens: 0,
		CostUSD:      0,
		AgentPID:     0,
	})

	if len(result.Triggered) != 4 {
		t.Fatalf("expected 4 triggered zero-value numeric rules, got %+v", result.Triggered)
	}
}

func TestEvaluate_ToolTaxonomyFieldsMatch(t *testing.T) {
	rules := []Commandment{
		{
			Name:        "tool-category-shell",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"tool_category": {Type: MatcherTypeExact, Pattern: "shell"},
			},
		},
		{
			Name:        "tool-effect-execute",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"tool_effect": {Type: MatcherTypeExact, Pattern: "execute"},
			},
		},
		{
			Name:        "tool-name-bash",
			Enforcement: "warn",
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"tool_name": {Type: MatcherTypeExact, Pattern: "Bash"},
			},
		},
	}

	compiled, err := compileRules(rules)
	if err != nil {
		t.Fatalf("compile rules: %v", err)
	}
	engine := &Engine{rules: compiled}

	result := engine.Evaluate(&audit.AuditEvent{ToolCategory: "shell", ToolEffect: "execute", ToolName: "Bash"})
	if len(result.Triggered) != 3 {
		t.Fatalf("expected 3 triggered taxonomy rules, got %+v", result.Triggered)
	}
}

func TestEvalLatencySLO(t *testing.T) {
	rules := make([]Commandment, 0, 20)
	for i := 0; i < 20; i++ {
		rules = append(rules, Commandment{
			Name:        fmt.Sprintf("rule-%02d", i),
			Enforcement: "warn",
			Priority:    100 - i,
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"action_type": {Type: MatcherTypeExact, Pattern: "command_execution"},
				"arguments":   {Type: MatcherTypeRegex, Pattern: fmt.Sprintf("cmd-%02d", i)},
			},
		})
	}

	compiled, err := compileRules(rules)
	if err != nil {
		t.Fatalf("compile rules: %v", err)
	}
	engine := &Engine{rules: compiled}

	events := make([]*audit.AuditEvent, 0, 10)
	for i := 0; i < 10; i++ {
		events = append(events, &audit.AuditEvent{
			ActionType: audit.ActionCommandExecution,
			Arguments:  fmt.Sprintf("run task cmd-%02d", i),
		})
	}

	const n = 10000
	durations := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		_ = engine.Evaluate(events[i%len(events)])
		durations[i] = time.Since(start)
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(n*95/100)-1]
	p99 := durations[(n*99/100)-1]

	if p95 >= 2*time.Millisecond {
		t.Fatalf("p95 too high: %s", p95)
	}
	if p99 >= 8*time.Millisecond {
		t.Fatalf("p99 too high: %s", p99)
	}
}

func BenchmarkEvaluate(b *testing.B) {
	rules := make([]Commandment, 0, 20)
	for i := 0; i < 20; i++ {
		rules = append(rules, Commandment{
			Name:        fmt.Sprintf("rule-%02d", i),
			Enforcement: "warn",
			Priority:    100 - i,
			Enabled:     boolPtr(true),
			Match: map[string]MatchCondition{
				"action_type": {Type: MatcherTypeExact, Pattern: "command_execution"},
				"arguments":   {Type: MatcherTypeRegex, Pattern: fmt.Sprintf("cmd-%02d", i)},
			},
		})
	}

	compiled, err := compileRules(rules)
	if err != nil {
		b.Fatalf("compile rules: %v", err)
	}
	engine := &Engine{rules: compiled}
	evt := &audit.AuditEvent{ActionType: audit.ActionCommandExecution, Arguments: "run task cmd-07"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.Evaluate(evt)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

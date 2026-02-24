package commandments

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

type TriggeredRule struct {
	Name        string `json:"name"`
	Enforcement string `json:"enforcement"`
	Message     string `json:"message,omitempty"`
}

type EvalResult struct {
	Evaluated []string        `json:"evaluated"`
	Triggered []TriggeredRule `json:"triggered"`
}

type RuleSummary struct {
	Name        string `json:"name"`
	Enforcement string `json:"enforcement"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
	Redact      bool   `json:"redact"`
	Message     string `json:"message,omitempty"`
}

type compiledCondition struct {
	field   string
	matcher Matcher
}

type compiledRule struct {
	rule       Commandment
	conditions []compiledCondition
}

func (r compiledRule) matches(e *audit.AuditEvent) bool {
	for _, c := range r.conditions {
		value, ok := eventFieldValue(e, c.field)
		if !ok || value == "" {
			return false
		}
		if !c.matcher.Match(value) {
			return false
		}
	}
	return true
}

type Engine struct {
	mu           sync.RWMutex
	rules        []compiledRule
	path         string
	fallbackYAML []byte
}

func NewEngine(path string, fallbackYAML []byte) (*Engine, error) {
	e := &Engine{path: path, fallbackYAML: fallbackYAML}
	if err := e.Reload(path); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Engine) Reload(path string) error {
	if path == "" {
		path = e.path
	}

	rs, err := loadRuleSet(path, e.fallbackYAML)
	if err != nil {
		return err
	}

	compiled, err := compileRules(rs.Commandments)
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.rules = compiled
	e.path = path
	e.mu.Unlock()

	return nil
}

func (e *Engine) Evaluate(evt *audit.AuditEvent) EvalResult {
	if evt == nil {
		return EvalResult{}
	}

	e.mu.RLock()
	rules := make([]compiledRule, len(e.rules))
	copy(rules, e.rules)
	e.mu.RUnlock()

	result := EvalResult{
		Evaluated: make([]string, 0, len(rules)),
		Triggered: make([]TriggeredRule, 0),
	}

	for _, r := range rules {
		if !r.rule.IsEnabled() {
			continue
		}

		result.Evaluated = append(result.Evaluated, r.rule.Name)
		if r.matches(evt) {
			result.Triggered = append(result.Triggered, TriggeredRule{
				Name:        r.rule.Name,
				Enforcement: r.rule.Enforcement,
				Message:     r.rule.Message,
			})
		}
	}

	return result
}

func (e *Engine) Rules() []RuleSummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]RuleSummary, 0, len(e.rules))
	for _, r := range e.rules {
		out = append(out, RuleSummary{
			Name:        r.rule.Name,
			Enforcement: r.rule.Enforcement,
			Priority:    r.rule.Priority,
			Enabled:     r.rule.IsEnabled(),
			Redact:      r.rule.Redact,
			Message:     r.rule.Message,
		})
	}
	return out
}

func loadRuleSet(path string, fallbackYAML []byte) (*RuleSet, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			return LoadYAML(data)
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if len(fallbackYAML) > 0 {
		return LoadYAML(fallbackYAML)
	}

	if path == "" {
		return nil, fmt.Errorf("no commandment source provided")
	}
	return nil, fmt.Errorf("commandment file not found: %s", path)
}

func compileRules(rules []Commandment) ([]compiledRule, error) {
	ruleCopy := make([]Commandment, len(rules))
	copy(ruleCopy, rules)

	sort.Slice(ruleCopy, func(i, j int) bool {
		if ruleCopy[i].Priority != ruleCopy[j].Priority {
			return ruleCopy[i].Priority > ruleCopy[j].Priority
		}
		return ruleCopy[i].Name < ruleCopy[j].Name
	})

	totalPatterns := 0
	compiled := make([]compiledRule, 0, len(ruleCopy))

	for _, r := range ruleCopy {
		fields := make([]string, 0, len(r.Match))
		for field := range r.Match {
			fields = append(fields, field)
		}
		sort.Strings(fields)

		conditions := make([]compiledCondition, 0, len(fields))
		for _, field := range fields {
			m, count, err := CompileMatcher(r.Match[field])
			if err != nil {
				return nil, fmt.Errorf("rule %q field %q: %w", r.Name, field, err)
			}
			totalPatterns += count
			if totalPatterns > MaxCompiledPatterns {
				return nil, fmt.Errorf("too many compiled patterns: %d > %d", totalPatterns, MaxCompiledPatterns)
			}
			conditions = append(conditions, compiledCondition{field: field, matcher: m})
		}

		compiled = append(compiled, compiledRule{rule: r, conditions: conditions})
	}

	return compiled, nil
}

func eventFieldValue(evt *audit.AuditEvent, field string) (string, bool) {
	switch field {
	case "id":
		return evt.ID, evt.ID != ""
	case "agent_id":
		return evt.AgentID, evt.AgentID != ""
	case "action_type":
		v := string(evt.ActionType)
		return v, v != ""
	case "action":
		return evt.Action, evt.Action != ""
	case "arguments":
		return evt.Arguments, evt.Arguments != ""
	case "session_id":
		return evt.SessionID, evt.SessionID != ""
	case "working_dir":
		return evt.WorkingDir, evt.WorkingDir != ""
	case "provider":
		return evt.Provider, evt.Provider != ""
	case "model":
		return evt.Model, evt.Model != ""
	case "tool_category":
		return evt.ToolCategory, evt.ToolCategory != ""
	case "tool_effect":
		return evt.ToolEffect, evt.ToolEffect != ""
	case "tool_name":
		return evt.ToolName, evt.ToolName != ""
	case "adapter_type":
		return evt.AdapterType, evt.AdapterType != ""
	case "adapter_id":
		return evt.AdapterID, evt.AdapterID != ""
	case "outcome":
		v := string(evt.Outcome)
		return v, v != ""
	case "input_tokens":
		return strconv.FormatInt(evt.InputTokens, 10), true
	case "output_tokens":
		return strconv.FormatInt(evt.OutputTokens, 10), true
	case "cost_usd":
		return strconv.FormatFloat(evt.CostUSD, 'f', -1, 64), true
	case "agent_pid":
		return strconv.Itoa(evt.AgentPID), true
	default:
		return "", false
	}
}

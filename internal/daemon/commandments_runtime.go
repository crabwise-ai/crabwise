package daemon

import (
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/commandments"
)

type CommandmentRuleSummary struct {
	Name        string `json:"name"`
	Enforcement string `json:"enforcement"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
}

type CommandmentsService interface {
	audit.Evaluator
	audit.Redactor
	List() []CommandmentRuleSummary
	Test(event *audit.AuditEvent) audit.EvalResult
	Reload() (int, error)
}

type CommandmentsFactory func(filePath string, fallbackYAML []byte) (CommandmentsService, error)

var NewCommandmentsService CommandmentsFactory = func(filePath string, fallbackYAML []byte) (CommandmentsService, error) {
	engine, err := commandments.NewEngine(filePath, fallbackYAML)
	if err != nil {
		return nil, err
	}

	return &commandmentsService{
		engine:   engine,
		redactor: commandments.NewRedactor(),
		path:     filePath,
	}, nil
}

type commandmentsService struct {
	engine   *commandments.Engine
	redactor *commandments.Redactor
	path     string
}

func (c *commandmentsService) Evaluate(event *audit.AuditEvent) audit.EvalResult {
	result := c.engine.Evaluate(event)

	triggered := make([]audit.TriggeredRule, 0, len(result.Triggered))
	for _, rule := range result.Triggered {
		triggered = append(triggered, audit.TriggeredRule{
			Name:        rule.Name,
			Enforcement: rule.Enforcement,
			Message:     rule.Message,
		})
	}

	return audit.EvalResult{
		Evaluated: append([]string(nil), result.Evaluated...),
		Triggered: triggered,
	}
}

func (c *commandmentsService) Redact(event *audit.AuditEvent, ruleTriggered bool) {
	c.redactor.Redact(event, ruleTriggered)
}

func (c *commandmentsService) List() []CommandmentRuleSummary {
	rules := c.engine.Rules()
	out := make([]CommandmentRuleSummary, 0, len(rules))
	for _, rule := range rules {
		out = append(out, CommandmentRuleSummary{
			Name:        rule.Name,
			Enforcement: rule.Enforcement,
			Priority:    rule.Priority,
			Enabled:     rule.Enabled,
		})
	}
	return out
}

func (c *commandmentsService) Test(event *audit.AuditEvent) audit.EvalResult {
	return c.Evaluate(event)
}

func (c *commandmentsService) Reload() (int, error) {
	if err := c.engine.Reload(c.path); err != nil {
		return 0, err
	}
	return len(c.engine.Rules()), nil
}

type noopCommandmentsService struct{}

func (n *noopCommandmentsService) Evaluate(event *audit.AuditEvent) audit.EvalResult {
	return audit.EvalResult{Evaluated: []string{}, Triggered: []audit.TriggeredRule{}}
}

func (n *noopCommandmentsService) Redact(event *audit.AuditEvent, ruleTriggered bool) {}

func (n *noopCommandmentsService) List() []CommandmentRuleSummary {
	return []CommandmentRuleSummary{}
}

func (n *noopCommandmentsService) Test(event *audit.AuditEvent) audit.EvalResult {
	return n.Evaluate(event)
}

func (n *noopCommandmentsService) Reload() (int, error) {
	return 0, nil
}

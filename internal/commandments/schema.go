package commandments

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersionV1        = "1"
	MaxRules               = 100
	MaxCompiledPatterns    = 200
	MaxPatternChars        = 1024
	MaxRedactionsPerField  = 50
	MaxRedactionFieldBytes = 1 << 20
)

const (
	MatcherTypeExact   = "exact"
	MatcherTypeRegex   = "regex"
	MatcherTypeGlob    = "glob"
	MatcherTypeNumeric = "numeric"
	MatcherTypeList    = "list"
)

type RuleSet struct {
	Version      string        `yaml:"version"`
	Commandments []Commandment `yaml:"commandments"`
}

type Commandment struct {
	Name        string                    `yaml:"name"`
	Description string                    `yaml:"description,omitempty"`
	Enforcement string                    `yaml:"enforcement"`
	Priority    int                       `yaml:"priority,omitempty"`
	Enabled     *bool                     `yaml:"enabled,omitempty"`
	Match       map[string]MatchCondition `yaml:"match"`
	Redact      bool                      `yaml:"redact,omitempty"`
	Message     string                    `yaml:"message,omitempty"`
}

func (c Commandment) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

type MatchCondition struct {
	Type     string      `yaml:"type,omitempty"`
	Pattern  string      `yaml:"pattern,omitempty"`
	Patterns []string    `yaml:"patterns,omitempty"`
	Op       string      `yaml:"op,omitempty"`
	Values   []string    `yaml:"values,omitempty"`
	Value    interface{} `yaml:"value,omitempty"`
}

func (m *MatchCondition) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var raw interface{}
		if err := node.Decode(&raw); err != nil {
			return err
		}
		m.Type = MatcherTypeExact
		m.Pattern = fmt.Sprint(raw)
		return nil
	case yaml.MappingNode:
		type alias MatchCondition
		var decoded alias
		if err := node.Decode(&decoded); err != nil {
			return err
		}
		*m = MatchCondition(decoded)
		return nil
	default:
		return fmt.Errorf("invalid matcher spec")
	}
}

func LoadFile(path string) (*RuleSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadYAML(data)
}

func LoadYAML(data []byte) (*RuleSet, error) {
	var rs RuleSet
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return nil, err
	}
	if err := rs.Validate(); err != nil {
		return nil, err
	}
	return &rs, nil
}

func (rs *RuleSet) Validate() error {
	if rs.Version != SchemaVersionV1 {
		return fmt.Errorf("unsupported version %q", rs.Version)
	}
	if len(rs.Commandments) > MaxRules {
		return fmt.Errorf("too many rules: %d > %d", len(rs.Commandments), MaxRules)
	}

	totalPatterns := 0
	seen := make(map[string]struct{}, len(rs.Commandments))

	for i := range rs.Commandments {
		r := rs.Commandments[i]
		if strings.TrimSpace(r.Name) == "" {
			return fmt.Errorf("rule[%d]: name is required", i)
		}
		if _, ok := seen[r.Name]; ok {
			return fmt.Errorf("rule[%d]: duplicate name %q", i, r.Name)
		}
		seen[r.Name] = struct{}{}

		switch r.Enforcement {
		case "warn", "block":
		default:
			return fmt.Errorf("rule[%d]: invalid enforcement %q", i, r.Enforcement)
		}

		if len(r.Match) == 0 {
			return fmt.Errorf("rule[%d]: match is required", i)
		}

		for field, spec := range r.Match {
			if strings.TrimSpace(field) == "" {
				return fmt.Errorf("rule[%d]: empty match field", i)
			}
			count, err := validateMatchCondition(field, spec)
			if err != nil {
				return fmt.Errorf("rule[%d] field %q: %w", i, field, err)
			}
			totalPatterns += count
			if totalPatterns > MaxCompiledPatterns {
				return fmt.Errorf("too many compiled patterns: %d > %d", totalPatterns, MaxCompiledPatterns)
			}
		}
	}

	return nil
}

func validateMatchCondition(_ string, spec MatchCondition) (int, error) {
	matcherType := strings.TrimSpace(spec.Type)
	if matcherType == "" {
		return 0, fmt.Errorf("type is required")
	}

	switch matcherType {
	case MatcherTypeExact:
		p := strings.TrimSpace(spec.Pattern)
		if p == "" {
			return 0, fmt.Errorf("pattern is required")
		}
		if err := validatePatternLen(p); err != nil {
			return 0, err
		}
		return 1, nil

	case MatcherTypeRegex:
		p := strings.TrimSpace(spec.Pattern)
		if p == "" {
			return 0, fmt.Errorf("pattern is required")
		}
		if err := validatePatternLen(p); err != nil {
			return 0, err
		}
		if _, err := regexp.Compile(p); err != nil {
			return 0, fmt.Errorf("invalid regex: %w", err)
		}
		return 1, nil

	case MatcherTypeGlob:
		patterns := make([]string, 0, 1+len(spec.Patterns))
		if strings.TrimSpace(spec.Pattern) != "" {
			patterns = append(patterns, strings.TrimSpace(spec.Pattern))
		}
		for _, p := range spec.Patterns {
			if strings.TrimSpace(p) != "" {
				patterns = append(patterns, strings.TrimSpace(p))
			}
		}
		if len(patterns) == 0 {
			return 0, fmt.Errorf("pattern or patterns is required")
		}
		for _, p := range patterns {
			if err := validatePatternLen(p); err != nil {
				return 0, err
			}
			if _, err := doublestar.Match(p, "x"); err != nil {
				return 0, fmt.Errorf("invalid glob: %w", err)
			}
		}
		return len(patterns), nil

	case MatcherTypeNumeric:
		switch spec.Op {
		case "gt", "lt", "eq", "gte", "lte":
		default:
			return 0, fmt.Errorf("invalid numeric op %q", spec.Op)
		}
		if _, err := parseNumericValue(spec.Value); err != nil {
			return 0, err
		}
		return 0, nil

	case MatcherTypeList:
		switch spec.Op {
		case "in", "not_in":
		default:
			return 0, fmt.Errorf("invalid list op %q", spec.Op)
		}
		if len(spec.Values) == 0 {
			return 0, fmt.Errorf("values is required")
		}
		for _, v := range spec.Values {
			if err := validatePatternLen(v); err != nil {
				return 0, err
			}
		}
		return len(spec.Values), nil

	default:
		return 0, fmt.Errorf("invalid matcher type %q", matcherType)
	}
}

func validatePatternLen(pattern string) error {
	if len(pattern) > MaxPatternChars {
		return fmt.Errorf("pattern too long: %d > %d", len(pattern), MaxPatternChars)
	}
	return nil
}

func parseNumericValue(v interface{}) (float64, error) {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return 0, fmt.Errorf("numeric value is required")
	}
	parsed, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value %q", s)
	}
	return parsed, nil
}

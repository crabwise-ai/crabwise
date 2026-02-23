package commandments

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type Matcher interface {
	Match(value string) bool
}

type ExactMatcher struct {
	expected string
}

func NewExactMatcher(expected string) *ExactMatcher {
	return &ExactMatcher{expected: expected}
}

func (m *ExactMatcher) Match(value string) bool {
	return value == m.expected
}

type RegexMatcher struct {
	re *regexp.Regexp
}

func NewRegexMatcher(pattern string) (*RegexMatcher, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &RegexMatcher{re: re}, nil
}

func (m *RegexMatcher) Match(value string) bool {
	return m.re.MatchString(value)
}

type GlobMatcher struct {
	pattern string
}

func NewGlobMatcher(pattern string) *GlobMatcher {
	return &GlobMatcher{pattern: pattern}
}

func (m *GlobMatcher) Match(value string) bool {
	matched, err := doublestar.Match(m.pattern, value)
	if err != nil {
		return false
	}
	return matched
}

type NumericMatcher struct {
	op     string
	target float64
}

func NewNumericMatcher(op string, target float64) *NumericMatcher {
	return &NumericMatcher{op: op, target: target}
}

func (m *NumericMatcher) Match(value string) bool {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return false
	}

	switch m.op {
	case "gt":
		return n > m.target
	case "lt":
		return n < m.target
	case "eq":
		return n == m.target
	case "gte":
		return n >= m.target
	case "lte":
		return n <= m.target
	default:
		return false
	}
}

type ListMatcher struct {
	values map[string]struct{}
	negate bool
}

func NewListMatcher(values []string, op string) *ListMatcher {
	m := make(map[string]struct{}, len(values))
	for _, v := range values {
		m[v] = struct{}{}
	}
	return &ListMatcher{values: m, negate: op == "not_in"}
}

func (m *ListMatcher) Match(value string) bool {
	_, ok := m.values[value]
	if m.negate {
		return !ok
	}
	return ok
}

type AnyMatcher struct {
	matchers []Matcher
}

func NewAnyMatcher(matchers []Matcher) *AnyMatcher {
	return &AnyMatcher{matchers: matchers}
}

func (m *AnyMatcher) Match(value string) bool {
	for _, matcher := range m.matchers {
		if matcher.Match(value) {
			return true
		}
	}
	return false
}

func CompileMatcher(spec MatchCondition) (Matcher, int, error) {
	switch spec.Type {
	case MatcherTypeExact:
		return NewExactMatcher(spec.Pattern), 1, nil

	case MatcherTypeRegex:
		m, err := NewRegexMatcher(spec.Pattern)
		if err != nil {
			return nil, 0, err
		}
		return m, 1, nil

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

		matchers := make([]Matcher, 0, len(patterns))
		for _, p := range patterns {
			matchers = append(matchers, NewGlobMatcher(p))
		}
		if len(matchers) == 1 {
			return matchers[0], 1, nil
		}
		return NewAnyMatcher(matchers), len(matchers), nil

	case MatcherTypeNumeric:
		target, err := parseNumericValue(spec.Value)
		if err != nil {
			return nil, 0, err
		}
		return NewNumericMatcher(spec.Op, target), 0, nil

	case MatcherTypeList:
		return NewListMatcher(spec.Values, spec.Op), len(spec.Values), nil
	}

	return nil, 0, fmt.Errorf("unsupported matcher type %q", spec.Type)
}

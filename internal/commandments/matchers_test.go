package commandments

import "testing"

func TestRegexMatcher_CaseSensitiveAndUnanchored(t *testing.T) {
	m, err := NewRegexMatcher("push.*main")
	if err != nil {
		t.Fatalf("compile regex: %v", err)
	}

	if !m.Match("git push origin main") {
		t.Fatal("expected unanchored regex to match substring")
	}
	if m.Match("git PUSH origin main") {
		t.Fatal("expected case-sensitive regex not to match")
	}
}

func TestGlobMatcher_CaseSensitive(t *testing.T) {
	m := NewGlobMatcher("**/.env")

	if !m.Match("/tmp/project/.env") {
		t.Fatal("expected glob to match")
	}
	if m.Match("/tmp/project/.ENV") {
		t.Fatal("expected case-sensitive glob not to match")
	}
}

func TestNumericMatcher_NonNumericNoMatch(t *testing.T) {
	m := NewNumericMatcher("gte", 100)

	if !m.Match("101") {
		t.Fatal("expected numeric match")
	}
	if m.Match("abc") {
		t.Fatal("non-numeric input must be no-match")
	}
}

func TestNumericMatcher_Ops(t *testing.T) {
	tests := []struct {
		op       string
		left     string
		target   float64
		expected bool
	}{
		{op: "gt", left: "11", target: 10, expected: true},
		{op: "lt", left: "9", target: 10, expected: true},
		{op: "eq", left: "10", target: 10, expected: true},
		{op: "gte", left: "10", target: 10, expected: true},
		{op: "lte", left: "10", target: 10, expected: true},
		{op: "gt", left: "10", target: 10, expected: false},
	}

	for _, tc := range tests {
		m := NewNumericMatcher(tc.op, tc.target)
		if got := m.Match(tc.left); got != tc.expected {
			t.Fatalf("op=%s left=%s target=%v expected=%v got=%v", tc.op, tc.left, tc.target, tc.expected, got)
		}
	}
}

func TestListMatcher_Exact(t *testing.T) {
	inMatcher := NewListMatcher([]string{"gpt-4o", "claude-sonnet"}, "in")
	if !inMatcher.Match("gpt-4o") {
		t.Fatal("expected exact match")
	}
	if inMatcher.Match("GPT-4O") {
		t.Fatal("expected case-sensitive exact list matching")
	}

	notInMatcher := NewListMatcher([]string{"blocked"}, "not_in")
	if !notInMatcher.Match("allowed") {
		t.Fatal("expected not_in matcher to pass")
	}
	if notInMatcher.Match("blocked") {
		t.Fatal("expected not_in matcher to fail for contained value")
	}
}

package commandments

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

func TestRedact_AlwaysOnPatterns(t *testing.T) {
	r := NewRedactor()

	tests := []string{
		`{"token":"` + openAITestToken() + `"}`,
		`{"aws":"` + awsAccessKeyLike() + `"}`,
		`{"gh":"` + githubPATLike() + `"}`,
		`{"slack":"` + slackTokenLike() + `"}`,
		`password=super-secret-value`,
	}

	for i, in := range tests {
		evt := &audit.AuditEvent{Arguments: in}
		r.Redact(evt, false)
		if !evt.Redacted {
			t.Fatalf("case %d expected redacted=true", i)
		}
		if !strings.Contains(evt.Arguments, redactionReplacement) {
			t.Fatalf("case %d expected replacement in %q", i, evt.Arguments)
		}
	}
}

func TestRedact_RuleTriggeredPatterns(t *testing.T) {
	r := NewRedactor()

	evtNoTrigger := &audit.AuditEvent{Arguments: "FOO=bar\nBAR=baz"}
	r.Redact(evtNoTrigger, false)
	if evtNoTrigger.Redacted {
		t.Fatal("did not expect redaction without trigger")
	}

	evtTriggered := &audit.AuditEvent{Arguments: "FOO=bar\nBAR=baz"}
	r.Redact(evtTriggered, true)
	if !evtTriggered.Redacted {
		t.Fatal("expected redaction when ruleTriggered=true")
	}
	if strings.Count(evtTriggered.Arguments, redactionReplacement) != 2 {
		t.Fatalf("expected 2 redactions, got %q", evtTriggered.Arguments)
	}
}

func TestRedact_ReplacementCap(t *testing.T) {
	r := NewRedactor()

	parts := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		parts = append(parts, openAITestToken()+fmt.Sprintf("%04d", i))
	}

	evt := &audit.AuditEvent{Arguments: strings.Join(parts, " ")}
	r.Redact(evt, false)

	if !evt.Redacted {
		t.Fatal("expected redacted=true")
	}
	if got := strings.Count(evt.Arguments, redactionReplacement); got != MaxRedactionsPerField {
		t.Fatalf("expected %d replacements, got %d", MaxRedactionsPerField, got)
	}
}

func TestRedact_OversizedSafeTruncation(t *testing.T) {
	r := NewRedactor()

	tailSecret := openAITestToken()
	in := strings.Repeat("A", MaxRedactionFieldBytes+4096) + tailSecret
	evt := &audit.AuditEvent{Arguments: in}
	r.Redact(evt, false)

	if !evt.Redacted {
		t.Fatal("expected redacted=true for oversized input")
	}
	if !strings.Contains(evt.Arguments, oversizedMarker) {
		t.Fatalf("expected oversized marker, got %q", evt.Arguments)
	}
	if strings.Contains(evt.Arguments, tailSecret) {
		t.Fatal("oversized tail secret should not be persisted")
	}
}

func openAITestToken() string {
	return "sk-" + strings.Repeat("a", 26)
}

func awsAccessKeyLike() string {
	return "AKIA" + strings.Repeat("A", 16)
}

func githubPATLike() string {
	return "ghp_" + strings.Repeat("a", 36)
}

func slackTokenLike() string {
	return "xoxb-" + strings.Repeat("1", 10) + "-" + strings.Repeat("a", 12)
}

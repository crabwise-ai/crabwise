package commandments

import (
	"regexp"
	"strings"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

const (
	redactionReplacement = "[REDACTED]"
	oversizedMarker      = "[REDACTED_OVERSIZE_TRUNCATED]"
	oversizedPreviewSize = 256
)

type Redactor struct {
	alwaysOn      []*regexp.Regexp
	ruleTriggered []*regexp.Regexp
}

func NewRedactor() *Redactor {
	return &Redactor{
		alwaysOn: []*regexp.Regexp{
			regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
			regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
			regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`),
			regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
			regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
			regexp.MustCompile(`(?i)\b(password|token|secret|api[_-]?key)\s*[:=]\s*['"]?[^'"\s,}]+`),
		},
		ruleTriggered: []*regexp.Regexp{
			regexp.MustCompile(`(?m)^[A-Za-z_][A-Za-z0-9_]*\s*=\s*.*$`),
			regexp.MustCompile(`(?i)authorization\s*:\s*bearer\s+[A-Za-z0-9._-]+`),
		},
	}
}

func (r *Redactor) Redact(evt *audit.AuditEvent, ruleTriggered bool) {
	if evt == nil {
		return
	}

	value := evt.Arguments
	redacted := false

	if len(value) > MaxRedactionFieldBytes {
		value = truncateOversized(value)
		redacted = true
	}

	remaining := MaxRedactionsPerField
	var changed bool
	value, changed, remaining = applyPatterns(value, r.alwaysOn, remaining)
	if changed {
		redacted = true
	}

	if ruleTriggered && remaining > 0 {
		value, changed, _ = applyPatterns(value, r.ruleTriggered, remaining)
		if changed {
			redacted = true
		}
	}

	if redacted {
		evt.Arguments = value
		evt.Redacted = true
	}
}

func applyPatterns(in string, patterns []*regexp.Regexp, remaining int) (string, bool, int) {
	out := in
	anyChanged := false

	for _, re := range patterns {
		if remaining <= 0 {
			break
		}
		next, replaced := replaceWithLimit(out, re, remaining)
		if replaced > 0 {
			anyChanged = true
			remaining -= replaced
			out = next
		}
	}

	return out, anyChanged, remaining
}

func replaceWithLimit(in string, re *regexp.Regexp, limit int) (string, int) {
	if limit <= 0 {
		return in, 0
	}

	matches := re.FindAllStringIndex(in, limit+1)
	if len(matches) == 0 {
		return in, 0
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}

	var b strings.Builder
	b.Grow(len(in))

	prev := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		b.WriteString(in[prev:start])
		b.WriteString(redactionReplacement)
		prev = end
	}
	b.WriteString(in[prev:])

	return b.String(), len(matches)
}

func truncateOversized(in string) string {
	preview := in
	if len(preview) > oversizedPreviewSize {
		preview = preview[:oversizedPreviewSize]
	}
	return preview + "..." + oversizedMarker
}

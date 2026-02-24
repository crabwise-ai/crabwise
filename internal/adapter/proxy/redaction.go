package proxy

import "regexp"

const maxRedactionsPerField = 50

var defaultRedactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`(?i)(password|token|secret)\s*[:=]\s*["']?[^\s"']+`),
}

func RedactPayload(raw []byte, extraPatterns []*regexp.Regexp) ([]byte, bool) {
	if len(raw) == 0 {
		return raw, false
	}

	patterns := defaultRedactionPatterns
	if len(extraPatterns) > 0 {
		patterns = append(append([]*regexp.Regexp(nil), patterns...), extraPatterns...)
	}

	redacted := string(raw)
	changed := false
	replacements := 0

	for _, re := range patterns {
		if replacements >= maxRedactionsPerField {
			break
		}
		next := re.ReplaceAllStringFunc(redacted, func(match string) string {
			if replacements >= maxRedactionsPerField {
				return match
			}
			replacements++
			return "[REDACTED]"
		})
		if next != redacted {
			changed = true
			redacted = next
		}
	}
	return []byte(redacted), changed
}

func CompilePatterns(patterns []string) []*regexp.Regexp {
	var compiled []*regexp.Regexp
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

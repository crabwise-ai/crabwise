package proxy

import "regexp"

var defaultRedactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`(?i)(password|token|secret)\s*[:=]\s*["']?[^\s"']+`),
}

func RedactPayload(raw []byte) ([]byte, bool) {
	if len(raw) == 0 {
		return raw, false
	}

	redacted := string(raw)
	changed := false
	for _, re := range defaultRedactionPatterns {
		next := re.ReplaceAllString(redacted, "[REDACTED]")
		if next != redacted {
			changed = true
			redacted = next
		}
	}
	return []byte(redacted), changed
}

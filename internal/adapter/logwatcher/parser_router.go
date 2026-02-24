package logwatcher

import (
	"encoding/json"
	"strings"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

var codexRecordTypes = map[string]struct{}{
	"session_meta":  {},
	"response_item": {},
	"event_msg":     {},
	"turn_context":  {},
	"token_count":   {},
}

type parserProbe struct {
	Type string `json:"type"`
}

func ParseLineForSource(line []byte, sourceFile string, lineOffset int64) ([]*audit.AuditEvent, error) {
	if isCodexSource(sourceFile) || looksLikeCodexLine(line) {
		return parseCodexLine(line, sourceFile, lineOffset)
	}
	return ParseLine(line, sourceFile, lineOffset)
}

func isCodexSource(sourceFile string) bool {
	if sourceFile == "" {
		return false
	}
	normalized := strings.ReplaceAll(sourceFile, "\\", "/")
	return strings.Contains(normalized, "/.codex/") || strings.Contains(normalized, "/codex/sessions/")
}

func looksLikeCodexLine(line []byte) bool {
	if len(line) == 0 {
		return false
	}
	var probe parserProbe
	if err := json.Unmarshal(line, &probe); err != nil {
		return false
	}
	_, ok := codexRecordTypes[probe.Type]
	return ok
}

package proxy

import (
	"encoding/json"
	"strings"
)

// ToolUseBlock is a fully-reconstructed tool invocation from an LLM response.
type ToolUseBlock struct {
	ID        string          // LLM-assigned tool call ID
	ToolName  string          // e.g. "Bash", "Write"
	ToolInput json.RawMessage // raw JSON arguments as the LLM emitted them
	Targets   ToolTargets     // parsed from ToolInput (see ParseTargets)
}

// ToolTargets holds structured enforcement targets parsed from tool_input.
type ToolTargets struct {
	Argv     []string // shell command argv (for Bash/computer tools)
	Paths    []string // file paths affected (for Write/Edit/Read tools)
	PathMode string   // "read" | "write" | "delete"
}

// ToolCallDelta is one streamed fragment of a tool call from the LLM.
// The Transport populates this from provider-specific SSE delta events.
type ToolCallDelta struct {
	Index     int    // position in tool_calls array (stable across deltas)
	ID        string // set only in the first delta for this call
	Name      string // set only in the first delta for this call
	ArgsDelta string // partial JSON string fragment to concatenate
}

// ParseTargets extracts structured enforcement targets from a tool's raw input.
// Supports Bash/computer (argv + paths from args), Write/Edit/Read (paths).
// Unknown tools return empty ToolTargets.
func ParseTargets(toolName string, input json.RawMessage) ToolTargets {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		return ToolTargets{}
	}

	switch strings.ToLower(toolName) {
	case "bash", "computer":
		return parseBashTargets(m)
	case "write", "multiedit":
		return parseFileTargets(m, "path", "write")
	case "edit", "notebookedit":
		return parseFileTargets(m, "file_path", "write")
	case "read", "glob", "grep":
		return parseFileTargets(m, "file_path", "read")
	}
	return ToolTargets{}
}

func parseBashTargets(m map[string]json.RawMessage) ToolTargets {
	var cmd string
	if v, ok := m["command"]; ok {
		_ = json.Unmarshal(v, &cmd)
	}
	if cmd == "" {
		return ToolTargets{}
	}
	argv := strings.Fields(cmd)
	paths := extractPathsFromArgv(argv)
	mode := inferPathMode(argv)
	return ToolTargets{Argv: argv, Paths: paths, PathMode: mode}
}

func parseFileTargets(m map[string]json.RawMessage, key, mode string) ToolTargets {
	var p string
	if v, ok := m[key]; ok {
		_ = json.Unmarshal(v, &p)
	}
	if p == "" {
		return ToolTargets{}
	}
	return ToolTargets{Paths: []string{p}, PathMode: mode}
}

// extractPathsFromArgv pulls non-flag arguments that look like paths.
func extractPathsFromArgv(argv []string) []string {
	var paths []string
	for _, arg := range argv {
		if len(arg) > 0 && arg[0] != '-' && strings.ContainsRune(arg, '/') {
			paths = append(paths, arg)
		}
	}
	return paths
}

// inferPathMode inspects argv for destructive delete indicators.
func inferPathMode(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	cmd := argv[0]
	switch cmd {
	case "rm", "rmdir", "unlink", "shred":
		return "delete"
	case "cat", "head", "tail", "less", "more", "wc", "grep", "find", "ls":
		return "read"
	default:
		return "write"
	}
}

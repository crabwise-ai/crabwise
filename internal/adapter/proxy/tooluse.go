package proxy

import "encoding/json"

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

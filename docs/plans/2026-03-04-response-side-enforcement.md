# Response-Side Proxy Enforcement Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend the HTTP proxy to evaluate LLM response tool_use blocks against commandments before forwarding to the agent, blocking harmful tool instructions at the source.

**Architecture:** Four phases — (1) new types + Transport interface extension, (2) OpenAI-specific parsing (non-streaming + streaming deltas), (3) proxy enforcement in both non-streaming and streaming handleProxy paths (streaming becomes full pre-buffer), (4) `gate.evaluate` IPC for external callers. The commandment engine (`Evaluator` interface) is unchanged; all new code is in the proxy normalization and IPC layers.
approved architecture design doc: `docs/plans/2026-03-03-local-enforcement-architecture-design.md`

**Tech Stack:** Go, `internal/adapter/proxy`, `internal/ipc`, `internal/daemon`, `internal/audit`, `internal/classify`

**Run all tests:** `eval "$(mise activate bash)" && go test -race ./...`

---

## User Stories

- As a security admin, I want `rm -rf /` blocked before Claude Code executes it, so the deletion never happens.
- As a Crabwise user, I want the same commandments to enforce across Claude Code, Codex, and OpenClaw without per-agent configuration.
- As a developer integrating a custom agent hook, I want to call `gate.evaluate` over the IPC socket and get a structured block/pass decision synchronously.

---

## Task 1: Define ToolUseBlock types and extend Transport interface

**Files:**
- Create: `internal/adapter/proxy/tooluse.go`
- Modify: `internal/adapter/proxy/provider.go`
- Modify: `internal/adapter/proxy/streaming_test.go`

**Step 1: Create `tooluse.go` with the new types**

```go
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
```

**Step 2: Extend `StreamEvent` in `provider.go` with ToolCallDeltas**

In `provider.go`, find the `StreamEvent` struct and add one field:

```go
type StreamEvent struct {
	Model          string
	FinishReason   string
	InputTokens    int64
	OutputTokens   int64
	HasUsage       bool
	HasFinish      bool
	EventType      string
	ToolCallDeltas []ToolCallDelta // NEW: streaming tool call fragments
}
```

**Step 3: Add `ExtractToolUseBlocks` to Transport interface in `provider.go`**

```go
type Transport interface {
	PrepareAuth(req *http.Request) error
	Forward(ctx context.Context, req *http.Request) (*http.Response, error)
	ParseStreamEvent(data []byte) (StreamEvent, error)
	// ExtractToolUseBlocks extracts complete tool_use blocks from a
	// non-streaming response body. Returns nil slice if none present.
	ExtractToolUseBlocks(body []byte) ([]ToolUseBlock, error)
}
```

**Step 4: Add stubs to the three mock transports in `streaming_test.go`**

Each of `mockStreamTransport`, `malformedJSONTransport`, `eventTypeCapture` needs:
```go
func (m *mockStreamTransport) ExtractToolUseBlocks(_ []byte) ([]ToolUseBlock, error) {
	return nil, nil
}
// (same stub for malformedJSONTransport and eventTypeCapture)
```

**Step 5: Add stub to `OpenAITransport` in `openai.go`**

```go
func (t *OpenAITransport) ExtractToolUseBlocks(_ []byte) ([]ToolUseBlock, error) {
	return nil, nil // real implementation in Task 3
}
```

**Step 6: Verify it compiles**

```bash
eval "$(mise activate bash)" && go build ./internal/adapter/proxy/...
```

Expected: no errors.

**Step 7: Commit**

```bash
git add internal/adapter/proxy/tooluse.go internal/adapter/proxy/provider.go internal/adapter/proxy/openai.go internal/adapter/proxy/streaming_test.go
git commit -m "feat: add ToolUseBlock types and extend Transport interface"
```

---

## Task 2: ParseTargets — argument parsers for v1 tools

Extract structured targets from raw `tool_input` JSON for the tools that matter in v1: `Bash`/`computer` (argv), `Write`/`Edit`/`Read` (paths).

**Files:**
- Modify: `internal/adapter/proxy/tooluse.go`
- Create: `internal/adapter/proxy/tooluse_test.go`

**Step 1: Write the failing tests**

```go
package proxy

import (
	"encoding/json"
	"testing"
)

func TestParseTargets_Bash(t *testing.T) {
	input := json.RawMessage(`{"command":"rm -rf /tmp/project"}`)
	got := ParseTargets("Bash", input)
	if len(got.Argv) == 0 || got.Argv[0] != "rm" {
		t.Fatalf("expected argv starting with rm, got %v", got.Argv)
	}
	// /tmp/project should appear in paths
	found := false
	for _, p := range got.Paths {
		if p == "/tmp/project" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected /tmp/project in paths, got %v", got.Paths)
	}
	if got.PathMode != "delete" {
		t.Fatalf("expected path_mode delete for rm -rf, got %q", got.PathMode)
	}
}

func TestParseTargets_Write(t *testing.T) {
	input := json.RawMessage(`{"path":"/foo/bar.go","content":"package main"}`)
	got := ParseTargets("Write", input)
	if len(got.Paths) != 1 || got.Paths[0] != "/foo/bar.go" {
		t.Fatalf("expected path /foo/bar.go, got %v", got.Paths)
	}
	if got.PathMode != "write" {
		t.Fatalf("expected path_mode write, got %q", got.PathMode)
	}
}

func TestParseTargets_Edit(t *testing.T) {
	input := json.RawMessage(`{"file_path":"/src/main.go","old_string":"x","new_string":"y"}`)
	got := ParseTargets("Edit", input)
	if len(got.Paths) != 1 || got.Paths[0] != "/src/main.go" {
		t.Fatalf("expected /src/main.go, got %v", got.Paths)
	}
	if got.PathMode != "write" {
		t.Fatalf("expected path_mode write, got %q", got.PathMode)
	}
}

func TestParseTargets_Read(t *testing.T) {
	input := json.RawMessage(`{"file_path":"/etc/passwd"}`)
	got := ParseTargets("Read", input)
	if len(got.Paths) != 1 || got.Paths[0] != "/etc/passwd" {
		t.Fatalf("expected /etc/passwd, got %v", got.Paths)
	}
	if got.PathMode != "read" {
		t.Fatalf("expected path_mode read, got %q", got.PathMode)
	}
}

func TestParseTargets_Unknown(t *testing.T) {
	input := json.RawMessage(`{"whatever":"value"}`)
	got := ParseTargets("UnknownTool", input)
	// should not panic; returns empty targets
	if len(got.Argv) != 0 || len(got.Paths) != 0 {
		t.Fatalf("expected empty targets for unknown tool, got %+v", got)
	}
}
```

**Step 2: Run to confirm failure**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestParseTargets -v
```

Expected: FAIL — `ParseTargets` undefined.

**Step 3: Implement `ParseTargets` in `tooluse.go`**

```go
import (
	"encoding/json"
	"strings"
)

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
```

**Step 4: Run tests**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestParseTargets -v
```

Expected: all 5 PASS.

**Step 5: Commit**

```bash
git add internal/adapter/proxy/tooluse.go internal/adapter/proxy/tooluse_test.go
git commit -m "feat: add ParseTargets argument parsers for Bash/Write/Edit/Read"
```

---

## Task 3: OpenAI non-streaming ExtractToolUseBlocks

**Files:**
- Modify: `internal/adapter/proxy/openai.go`
- Create: `internal/adapter/proxy/openai_tooluse_test.go`

**Step 1: Write failing tests**

```go
package proxy

import (
	"encoding/json"
	"testing"
)

func TestOpenAIExtractToolUseBlocks_SingleToolCall(t *testing.T) {
	body := []byte(`{
		"choices": [{
			"message": {
				"role": "assistant",
				"tool_calls": [{
					"id": "call_abc",
					"type": "function",
					"function": {
						"name": "Bash",
						"arguments": "{\"command\":\"rm -rf /tmp/test\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)

	tr := NewOpenAITransport(ProviderConfig{}, 0)
	blocks, err := tr.ExtractToolUseBlocks(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].ID != "call_abc" {
		t.Errorf("expected id call_abc, got %q", blocks[0].ID)
	}
	if blocks[0].ToolName != "Bash" {
		t.Errorf("expected tool Bash, got %q", blocks[0].ToolName)
	}
	if len(blocks[0].Targets.Argv) == 0 {
		t.Error("expected argv parsed from command")
	}
}

func TestOpenAIExtractToolUseBlocks_NoToolCalls(t *testing.T) {
	body := []byte(`{
		"choices": [{
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}]
	}`)

	tr := NewOpenAITransport(ProviderConfig{}, 0)
	blocks, err := tr.ExtractToolUseBlocks(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestOpenAIExtractToolUseBlocks_MalformedArguments(t *testing.T) {
	body := []byte(`{
		"choices": [{
			"message": {
				"tool_calls": [{
					"id": "call_x",
					"function": {"name": "Bash", "arguments": "not-json"}
				}]
			}
		}]
	}`)

	tr := NewOpenAITransport(ProviderConfig{}, 0)
	blocks, err := tr.ExtractToolUseBlocks(body)
	// Should not error — malformed args result in a block with raw input preserved.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block even with malformed args, got %d", len(blocks))
	}
}
```

**Step 2: Run to confirm failure**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestOpenAIExtractToolUseBlocks -v
```

Expected: FAIL — method returns nil, nil.

**Step 3: Implement `ExtractToolUseBlocks` in `openai.go`**

Replace the stub with:

```go
func (t *OpenAITransport) ExtractToolUseBlocks(body []byte) ([]ToolUseBlock, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, nil
	}
	var blocks []ToolUseBlock
	for _, choice := range resp.Choices {
		for _, tc := range choice.Message.ToolCalls {
			rawInput := json.RawMessage(tc.Function.Arguments)
			// Preserve malformed args as {"_raw_args": "<original>"} for forensic context.
			if !json.Valid(rawInput) {
				escaped, _ := json.Marshal(string(rawInput))
				rawInput = json.RawMessage(`{"_raw_args":` + string(escaped) + `}`)
			}
			blocks = append(blocks, ToolUseBlock{
				ID:        tc.ID,
				ToolName:  tc.Function.Name,
				ToolInput: rawInput,
				Targets:   ParseTargets(tc.Function.Name, rawInput),
			})
		}
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	return blocks, nil
}
```

**Step 4: Run tests**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestOpenAIExtractToolUseBlocks -v
```

Expected: all 3 PASS.

**Step 5: Commit**

```bash
git add internal/adapter/proxy/openai.go internal/adapter/proxy/openai_tooluse_test.go
git commit -m "feat: implement OpenAI non-streaming ExtractToolUseBlocks"
```

---

## Task 4: OpenAI streaming ToolCallDelta in ParseStreamEvent

**Files:**
- Modify: `internal/adapter/proxy/openai.go`
- Create: `internal/adapter/proxy/openai_stream_tooluse_test.go`

**Step 1: Write failing tests**

```go
package proxy

import "testing"

func TestOpenAIParseStreamEvent_FirstToolCallChunk(t *testing.T) {
	data := []byte(`{
		"choices": [{
			"delta": {
				"tool_calls": [{
					"index": 0,
					"id": "call_abc",
					"type": "function",
					"function": {"name": "Bash", "arguments": ""}
				}]
			}
		}]
	}`)
	tr := NewOpenAITransport(ProviderConfig{}, 0)
	event, err := tr.ParseStreamEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.ToolCallDeltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(event.ToolCallDeltas))
	}
	d := event.ToolCallDeltas[0]
	if d.Index != 0 {
		t.Errorf("expected index 0, got %d", d.Index)
	}
	if d.ID != "call_abc" {
		t.Errorf("expected id call_abc, got %q", d.ID)
	}
	if d.Name != "Bash" {
		t.Errorf("expected name Bash, got %q", d.Name)
	}
}

func TestOpenAIParseStreamEvent_ArgumentFragment(t *testing.T) {
	data := []byte(`{
		"choices": [{
			"delta": {
				"tool_calls": [{
					"index": 0,
					"function": {"arguments": "{\"comma"}
				}]
			}
		}]
	}`)
	tr := NewOpenAITransport(ProviderConfig{}, 0)
	event, err := tr.ParseStreamEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.ToolCallDeltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(event.ToolCallDeltas))
	}
	if event.ToolCallDeltas[0].ArgsDelta != `{"comma` {
		t.Errorf("expected args fragment, got %q", event.ToolCallDeltas[0].ArgsDelta)
	}
}

func TestOpenAIParseStreamEvent_NoToolCalls(t *testing.T) {
	data := []byte(`{"choices": [{"delta": {"content": "Hello"}}]}`)
	tr := NewOpenAITransport(ProviderConfig{}, 0)
	event, err := tr.ParseStreamEvent(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.ToolCallDeltas) != 0 {
		t.Fatalf("expected 0 deltas, got %d", len(event.ToolCallDeltas))
	}
}
```

**Step 2: Run to confirm failure**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestOpenAIParseStreamEvent_ -v
```

Expected: FAIL — ToolCallDeltas always empty.

**Step 3: Extend `ParseStreamEvent` in `openai.go`**

Add tool_call delta extraction to the existing `ParseStreamEvent` function. After the existing `choices` block:

```go
// Extract tool call deltas (streaming tool_use)
if len(choices) > 0 {
	if c0, ok := choices[0].(map[string]interface{}); ok {
		if delta, ok := c0["delta"].(map[string]interface{}); ok {
			if rawCalls, ok := delta["tool_calls"].([]interface{}); ok {
				for _, rawCall := range rawCalls {
					call, ok := rawCall.(map[string]interface{})
					if !ok {
						continue
					}
					d := ToolCallDelta{}
					if idx, ok := call["index"]; ok {
						d.Index = int(toInt64(idx))
					}
					if id, ok := call["id"].(string); ok {
						d.ID = id
					}
					if fn, ok := call["function"].(map[string]interface{}); ok {
						if name, ok := fn["name"].(string); ok {
							d.Name = name
						}
						if args, ok := fn["arguments"].(string); ok {
							d.ArgsDelta = args
						}
					}
					out.ToolCallDeltas = append(out.ToolCallDeltas, d)
				}
			}
		}
	}
}
```

**Step 4: Run tests**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestOpenAIParseStreamEvent_ -v
```

Expected: all 3 PASS.

**Step 5: Run full proxy tests to make sure nothing regressed**

```bash
eval "$(mise activate bash)" && go test -race ./internal/adapter/proxy/...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/adapter/proxy/openai.go internal/adapter/proxy/openai_stream_tooluse_test.go
git commit -m "feat: populate ToolCallDeltas in OpenAI ParseStreamEvent"
```

---

## Task 5: evaluateToolUseBlocks helper on Proxy

**Files:**
- Modify: `internal/adapter/proxy/proxy.go`
- Create: `internal/adapter/proxy/tooluse_eval_test.go`

**Step 1: Write failing tests**

```go
package proxy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

type stubEvaluator struct {
	blockTool string // if set, block any tool_use with this ToolName
}

type noopClassifier struct{}

func (n *noopClassifier) Classify(provider, toolName string, argKeys []string) classify.Classification {
	return classify.Classification{}
}

func (s *stubEvaluator) Evaluate(e *audit.AuditEvent) audit.EvalResult {
	if s.blockTool != "" && e.ToolName == s.blockTool {
		return audit.EvalResult{
			Evaluated: []string{"test-commandment"},
			Triggered: []audit.TriggeredRule{{
				Name:        "test-commandment",
				Enforcement: "block",
				Message:     "blocked by test",
			}},
		}
	}
	return audit.EvalResult{}
}

func TestEvaluateToolUseBlocks_Blocked(t *testing.T) {
	p := &Proxy{evaluator: &stubEvaluator{blockTool: "Bash"}, classifier: &noopClassifier{}}
	blocks := []ToolUseBlock{{
		ID:        "call_1",
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`{"command":"rm -rf /"}`),
		Targets:   ParseTargets("Bash", json.RawMessage(`{"command":"rm -rf /"}`)),
	}}
	blocked, commandmentID := p.evaluateToolUseBlocks(blocks, "openai", "gpt-4o", time.Now())
	if !blocked {
		t.Fatal("expected blocked=true")
	}
	if commandmentID == "" {
		t.Fatal("expected commandment_id to be set")
	}
}

func TestEvaluateToolUseBlocks_NotBlocked(t *testing.T) {
	p := &Proxy{evaluator: &stubEvaluator{}, classifier: &noopClassifier{}}
	blocks := []ToolUseBlock{{
		ID:       "call_1",
		ToolName: "Read",
		ToolInput: json.RawMessage(`{"file_path":"/src/main.go"}`),
	}}
	blocked, _ := p.evaluateToolUseBlocks(blocks, "openai", "gpt-4o", time.Now())
	if blocked {
		t.Fatal("expected blocked=false")
	}
}

func TestEvaluateToolUseBlocks_Empty(t *testing.T) {
	p := &Proxy{evaluator: &stubEvaluator{blockTool: "Bash"}, classifier: &noopClassifier{}}
	blocked, _ := p.evaluateToolUseBlocks(nil, "openai", "gpt-4o", time.Now())
	if blocked {
		t.Fatal("expected blocked=false for empty blocks")
	}
}
```

**Step 2: Run to confirm failure**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestEvaluateToolUseBlocks -v
```

Expected: FAIL — `evaluateToolUseBlocks` undefined.

**Step 3: Implement `evaluateToolUseBlocks` in `proxy.go`**

Add after `shouldBlock`:

```go
// evaluateToolUseBlocks evaluates each tool_use block from an LLM response.
// Returns (true, commandmentID) if any block is blocked; emits audit events for blocked blocks.
// Returns (false, "") if all pass.
func (p *Proxy) evaluateToolUseBlocks(blocks []ToolUseBlock, provider, model string, ts time.Time) (bool, string) {
	if p.evaluator == nil {
		return false, ""
	}
	for _, block := range blocks {
		argKeys := classify.ExtractArgKeys(block.ToolInput)
		var cls classify.Classification
		if p.classifier != nil {
			cls = p.classifier.Classify(provider, block.ToolName, argKeys)
		}

		e := &audit.AuditEvent{
			ID:                   uuid.NewString(),
			Timestamp:            ts,
			AgentID:              "proxy",
			ActionType:           audit.ActionToolCall,
			Action:               block.ToolName,
			Provider:             provider,
			Model:                model,
			ToolName:             block.ToolName,
			ToolCategory:         cls.Category,
			ToolEffect:           cls.Effect,
			TaxonomyVersion:      cls.TaxonomyVersion,
			ClassificationSource: cls.ClassificationSource,
			AdapterID:            "proxy",
			AdapterType:          "proxy",
		}
		p.appendArgumentMetadata(e, map[string]interface{}{
			"tool_call_id": block.ID,
			"tool_input":   block.ToolInput, // raw args for argument-sensitive commandment rules
			"targets":      block.Targets,
		})

		result := p.evaluator.Evaluate(e)
		for _, triggered := range result.Triggered {
			if strings.EqualFold(triggered.Enforcement, "block") {
				e.Outcome = audit.OutcomeBlocked
				p.emit(e)
				return true, triggered.Name
			}
		}
	}
	return false, ""
}
```

Note: this method needs `classify` and `uuid` imports — add them to proxy.go import block if not already present. `classify` is `github.com/crabwise-ai/crabwise/internal/classify`.

**Step 4: Run tests**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestEvaluateToolUseBlocks -v
```

Expected: all 3 PASS.

**Step 5: Run all proxy tests**

```bash
eval "$(mise activate bash)" && go test -race ./internal/adapter/proxy/...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/adapter/proxy/proxy.go internal/adapter/proxy/tooluse_eval_test.go
git commit -m "feat: add evaluateToolUseBlocks helper to Proxy"
```

---

## Task 6: Non-streaming response-side enforcement

**Files:**
- Modify: `internal/adapter/proxy/proxy.go`
- Create: `internal/adapter/proxy/proxy_response_enforce_test.go`

**Step 1: Write failing integration tests**

Use a mock upstream via `httptest.NewServer`. The test wires a real `Proxy` with a mock evaluator.

```go
package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

// blockBashEvaluator blocks any tool named "Bash".
type blockBashEvaluator struct{}

func (b *blockBashEvaluator) Evaluate(e *audit.AuditEvent) audit.EvalResult {
	if e.ToolName == "Bash" {
		return audit.EvalResult{
			Triggered: []audit.TriggeredRule{{Name: "no-bash", Enforcement: "block"}},
		}
	}
	return audit.EvalResult{}
}

func newTestProxy(t *testing.T, upstream *httptest.Server, eval Evaluator) *Proxy {
	t.Helper()
	cfg := Config{
		Listen:          "127.0.0.1:0",
		DefaultProvider: "openai",
		MaxRequestBody:  1 << 20,
		Providers: map[string]ProviderConfig{
			"openai": {
				Name:            "openai",
				UpstreamBaseURL: upstream.URL,
				AuthMode:        "passthrough",
				RoutePatterns:   []string{"api.openai.com"},
			},
		},
	}
	p, err := New(cfg, eval, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestHandleProxy_NonStreaming_BlockedToolUse(t *testing.T) {
	toolCallResp := `{
		"id": "chatcmpl-1",
		"choices": [{
			"message": {
				"role": "assistant",
				"tool_calls": [{"id":"call_1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"rm -rf /\"}"}}]
			},
			"finish_reason": "tool_calls"
		}],
		"model": "gpt-4o"
	}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(toolCallResp))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream, &blockBashEvaluator{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "api.openai.com"
	rr := httptest.NewRecorder()

	p.handleProxy(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
	var errResp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("expected JSON error response: %v", err)
	}
	errObj := errResp["error"].(map[string]interface{})
	if errObj["type"] != "policy_violation" {
		t.Errorf("expected error type policy_violation, got %v", errObj["type"])
	}
}

func TestHandleProxy_NonStreaming_AllowedToolUse(t *testing.T) {
	toolCallResp := `{
		"id": "chatcmpl-2",
		"choices": [{
			"message": {
				"role": "assistant",
				"tool_calls": [{"id":"call_2","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"/src/main.go\"}"}}]
			},
			"finish_reason": "tool_calls"
		}],
		"model": "gpt-4o"
	}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(toolCallResp))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream, &blockBashEvaluator{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "api.openai.com"
	rr := httptest.NewRecorder()

	p.handleProxy(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}
```

**Step 2: Run to confirm failure**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestHandleProxy_NonStreaming -v
```

Expected: FAIL — proxy forwards response without evaluating tool_use blocks.

**Step 3: Add response-side enforcement to `handleProxy` non-streaming branch**

In `handleProxy`, find the non-streaming branch (the `else` block after streaming). After `io.ReadAll(upstreamResp.Body)` and before `sendUpstreamHeaders`, add:

```go
// Response-side tool_use enforcement (non-streaming)
if len(respBody) > 0 && upstreamResp.StatusCode < 400 {
	toolUseBlocks, extractErr := providerRuntime.Transport.ExtractToolUseBlocks(respBody)
	if extractErr != nil {
		writeProxyError(w, http.StatusBadGateway, "enforcement_error",
			"failed to extract tool_use blocks: "+extractErr.Error(), eventID)
		preflight.Outcome = audit.OutcomeFailure
		p.emit(preflight)
		return
	}
	if blocked, commandmentID := p.evaluateToolUseBlocks(toolUseBlocks, providerName, normalizedReq.Model, start); blocked {
		p.metrics.TotalBlocked.Add(1)
		writeProxyError(w, http.StatusForbidden, "policy_violation",
			"tool_use blocked by commandment: "+commandmentID, eventID)
		preflight.Outcome = audit.OutcomeBlocked
		p.appendArgumentMetadata(preflight, map[string]interface{}{
			"blocked_commandment": commandmentID,
			"enforcement":         "response_side",
		})
		p.emit(preflight)
		return
	}
}
```

Place this block immediately after `respBody, readErr := io.ReadAll(upstreamResp.Body)`.

**Step 4: Run tests**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestHandleProxy_NonStreaming -v
```

Expected: both PASS.

**Step 5: Run all proxy tests**

```bash
eval "$(mise activate bash)" && go test -race ./internal/adapter/proxy/...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/adapter/proxy/proxy.go internal/adapter/proxy/proxy_response_enforce_test.go
git commit -m "feat: response-side enforcement for non-streaming LLM responses"
```

---

## Task 7: Streaming pre-buffer + enforcement

This replaces `proxySSEStream`'s pass-through with full pre-buffer. All SSE bytes are collected, tool_call deltas assembled into `ToolUseBlock`s, evaluated, then either forwarded or blocked.

**Files:**
- Modify: `internal/adapter/proxy/streaming.go`
- Modify: `internal/adapter/proxy/streaming_test.go` (update existing tests)
- Modify: `internal/adapter/proxy/proxy.go` (update streaming branch)
- Create: `internal/adapter/proxy/streaming_enforce_test.go`

**Step 1: Write new streaming enforcement test**

```go
package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleProxy_Streaming_BlockedToolUse(t *testing.T) {
	sseBody := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Bash","arguments":""}}]}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":\"rm -rf /\"}"}}]}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, sseBody)
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream, &blockBashEvaluator{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "api.openai.com"
	rr := httptest.NewRecorder()

	p.handleProxy(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleProxy_Streaming_AllowedNoToolUse(t *testing.T) {
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, sseBody)
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream, &blockBashEvaluator{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "api.openai.com"
	rr := httptest.NewRecorder()

	p.handleProxy(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Hello") {
		t.Error("expected forwarded body to contain streamed content")
	}
}
```

**Step 2: Run to confirm the streaming blocked test fails**

```bash
eval "$(mise activate bash)" && go test ./internal/adapter/proxy/ -run TestHandleProxy_Streaming -v
```

Expected: FAIL.

**Step 3: Add `bufferSSEStream` to `streaming.go`**

Add a new function alongside `proxySSEStream`:

```go
const defaultStreamMaxBytes = 10 * 1024 * 1024 // 10 MB

type bufferedSSEResult struct {
	Body       []byte
	Telemetry  streamTelemetry
	ToolBlocks []ToolUseBlock
}

// bufferSSEStream reads the entire SSE stream into memory, collecting telemetry
// and reconstructing tool_use blocks from ToolCallDeltas. It does NOT write to w.
// Returns enforcement_error if body exceeds maxBytes.
func bufferSSEStream(src io.ReadCloser, transport Transport, idleTimeout time.Duration, maxBytes int64) (bufferedSSEResult, error) {
	defer src.Close()

	var result bufferedSSEResult
	var bodyBuf []byte
	var pendingEventType string

	// tool call accumulator: index -> {id, name, args}
	type tcAcc struct{ id, name string; args strings.Builder }
	accumulators := map[int]*tcAcc{}
	var toolCallsComplete bool

	lines := make(chan streamLine, 64)
	go func() {
		defer close(lines)
		reader := bufio.NewReader(src)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				cpy := make([]byte, len(line))
				copy(cpy, line)
				lines <- streamLine{data: cpy}
			}
			if err != nil {
				if err == io.EOF {
					return
				}
				lines <- streamLine{err: err}
				return
			}
		}
	}()

	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return result, fmt.Errorf("stream idle timeout")
		case msg, ok := <-lines:
			if !ok {
				// Stream ended — finalize tool_use blocks if any
				if toolCallsComplete || len(accumulators) > 0 {
					result.ToolBlocks = finalizeToolUseBlocks(accumulators)
				}
				result.Body = bodyBuf
				return result, nil
			}
			if msg.err != nil {
				return result, msg.err
			}

			// Enforce buffer limit
			if int64(len(bodyBuf)+len(msg.data)) > maxBytes {
				return result, fmt.Errorf("stream buffer exceeded %d bytes", maxBytes)
			}
			bodyBuf = append(bodyBuf, msg.data...)

			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idleTimeout)

			if eventType := parseSSEEventType(msg.data); eventType != "" {
				pendingEventType = eventType
				continue
			}

			payload := parseSSEData(msg.data)
			if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
				continue
			}

			if result.Telemetry.FirstTokenAt.IsZero() {
				result.Telemetry.FirstTokenAt = time.Now()
			}

			event, err := transport.ParseStreamEvent(payload)
			if err != nil {
				// Fail closed: a parse error may mean a tool_call delta was dropped.
				// Return enforcement_error so the caller can return HTTP 502.
				return result, fmt.Errorf("enforcement_error: failed to parse stream event: %w", err)
			}
			event.EventType = pendingEventType
			pendingEventType = ""

			if event.Model != "" {
				result.Telemetry.Model = event.Model
			}
			if event.HasFinish {
				result.Telemetry.FinishReason = event.FinishReason
				if event.FinishReason == "tool_calls" {
					toolCallsComplete = true
				}
			}
			if event.HasUsage {
				result.Telemetry.InputTokens = event.InputTokens
				result.Telemetry.OutputTokens = event.OutputTokens
				result.Telemetry.HasUsage = true
			}

			// Accumulate tool call deltas
			for _, d := range event.ToolCallDeltas {
				acc, ok := accumulators[d.Index]
				if !ok {
					acc = &tcAcc{}
					accumulators[d.Index] = acc
				}
				if d.ID != "" {
					acc.id = d.ID
				}
				if d.Name != "" {
					acc.name = d.Name
				}
				acc.args.WriteString(d.ArgsDelta)
			}
		}
	}
}

func finalizeToolUseBlocks(accumulators map[int]*struct{ id, name string; args strings.Builder }) []ToolUseBlock {
	// Note: accumulators type is aliased in bufferSSEStream — adjust to match actual type
	blocks := make([]ToolUseBlock, 0, len(accumulators))
	for _, acc := range accumulators {
		rawInput := json.RawMessage(acc.args.String())
		if !json.Valid(rawInput) {
			rawInput = json.RawMessage(`{}`)
		}
		blocks = append(blocks, ToolUseBlock{
			ID:        acc.id,
			ToolName:  acc.name,
			ToolInput: rawInput,
			Targets:   ParseTargets(acc.name, rawInput),
		})
	}
	return blocks
}
```

Note: `finalizeToolUseBlocks` needs the concrete `tcAcc` type. In practice, extract `tcAcc` as a package-level unexported type or define `finalizeToolUseBlocks` as a closure inside `bufferSSEStream`. Adjust to compile cleanly.

Add required imports to streaming.go: `"bytes"`, `"encoding/json"`, `"strings"`.

**Step 4: Update `handleProxy` streaming branch in `proxy.go`**

Replace the current streaming block:
```go
// OLD:
sendUpstreamHeaders(upstreamResp.StatusCode)
streamTel, streamErr := proxySSEStream(w, upstreamResp.Body, providerRuntime.Transport, p.cfg.StreamIdleTimeout)
```

With:
```go
// NEW — pre-buffer for enforcement, then forward
maxBuf := p.cfg.StreamMaxBuffer
if maxBuf <= 0 {
    maxBuf = defaultStreamMaxBytes
}
buffered, streamErr := bufferSSEStream(upstreamResp.Body, providerRuntime.Transport, p.cfg.StreamIdleTimeout, maxBuf)
if streamErr != nil {
    if strings.Contains(streamErr.Error(), "stream buffer exceeded") {
        writeProxyError(w, http.StatusBadGateway, "enforcement_error", streamErr.Error(), eventID)
    } else {
        p.metrics.UpstreamErrors.Add(1)
        writeProxyError(w, http.StatusBadGateway, "upstream_error", streamErr.Error(), eventID)
    }
    preflight.Outcome = audit.OutcomeFailure
    p.emit(preflight)
    return
}

// Evaluate tool_use blocks from the buffered stream
if blocked, commandmentID := p.evaluateToolUseBlocks(buffered.ToolBlocks, providerName, normalizedReq.Model, start); blocked {
    p.metrics.TotalBlocked.Add(1)
    writeProxyError(w, http.StatusForbidden, "policy_violation",
        "tool_use blocked by commandment: "+commandmentID, eventID)
    preflight.Outcome = audit.OutcomeBlocked
    p.appendArgumentMetadata(preflight, map[string]interface{}{
        "blocked_commandment": commandmentID,
        "enforcement":         "response_side_stream",
    })
    p.emit(preflight)
    return
}

// Forward buffered stream to client
sendUpstreamHeaders(upstreamResp.StatusCode)
if _, err := w.Write(buffered.Body); err != nil {
    p.metrics.UpstreamErrors.Add(1)
}
if flusher, ok := w.(http.Flusher); ok {
    flusher.Flush()
}

streamTel := buffered.Telemetry
```

Also add `StreamMaxBuffer int64` to `Config` struct in `provider.go`.

**Step 5: Update existing streaming tests in `streaming_test.go`**

The `TestProxySSEStream_*` tests tested `proxySSEStream` directly (pass-through). They now need to test `bufferSSEStream`. Update each test:
- Change call from `proxySSEStream(rec, pr, tr, ...)` to `bufferSSEStream(pr, tr, ...)`
- Instead of checking `rec.Body`, check `result.Body` and `result.Telemetry`
- Remove tests that checked incremental writes / flush counts (those semantics no longer apply)

Example update for `TestProxySSEStream_NormalFlow`:
```go
func TestBufferSSEStream_NormalFlow(t *testing.T) {
	// ... setup same pr/pw pipe ...
	result, sErr := bufferSSEStream(pr, tr, 5*time.Second, defaultStreamMaxBytes)
	// check result.Telemetry.Model, result.Body, etc.
}
```

Rename all `TestProxySSEStream_*` → `TestBufferSSEStream_*`.

**Step 6: Run tests**

```bash
eval "$(mise activate bash)" && go test -race ./internal/adapter/proxy/ -run "TestHandleProxy_Streaming|TestBufferSSEStream" -v
```

Expected: all PASS.

**Step 7: Run full test suite**

```bash
eval "$(mise activate bash)" && go test -race ./...
```

Expected: PASS.

**Step 8: Commit**

```bash
git add internal/adapter/proxy/streaming.go internal/adapter/proxy/streaming_test.go internal/adapter/proxy/proxy.go internal/adapter/proxy/streaming_enforce_test.go internal/adapter/proxy/provider.go
git commit -m "feat: replace pass-through streaming with pre-buffer + response-side enforcement"
```

---

## Task 8: gate.evaluate IPC method

Exposes the commandment engine to external callers (e.g., a Claude Code `PreToolUse` hook) via a JSON-RPC method on the daemon's Unix socket.

**Files:**
- Create: `internal/daemon/gate.go`
- Modify: `internal/daemon/daemon.go` (wire the handler)
- Create: `internal/daemon/gate_test.go`

**Step 1: Write failing tests for the gate handler**

```go
package daemon

import (
	"encoding/json"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

type stubCommandments struct{}

func (s *stubCommandments) Evaluate(e *audit.AuditEvent) audit.EvalResult {
	if e.ToolName == "Bash" {
		return audit.EvalResult{
			Triggered: []audit.TriggeredRule{{Name: "no-bash", Enforcement: "block", Message: "bash blocked"}},
		}
	}
	return audit.EvalResult{}
}
func (s *stubCommandments) Redact(*audit.AuditEvent, bool) {}
func (s *stubCommandments) List() []CommandmentRuleSummary  { return nil }
func (s *stubCommandments) Test(e *audit.AuditEvent) audit.EvalResult { return s.Evaluate(e) }
func (s *stubCommandments) Reload() (int, error)            { return 0, nil }

func TestGateEvaluateHandler_Block(t *testing.T) {
	handler := makeGateEvaluateHandler(&stubCommandments{})
	params, _ := json.Marshal(GateEvaluateParams{
		AgentID:   "claude-code",
		ToolName:  "Bash",
		ToolCategory: "shell",
		ToolEffect:   "write",
	})
	result, err := handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := result.(GateEvaluateResult)
	if res.Decision != "block" {
		t.Fatalf("expected decision block, got %q", res.Decision)
	}
	if res.CommandmentID != "no-bash" {
		t.Errorf("expected commandment_id no-bash, got %q", res.CommandmentID)
	}
	if res.GateEventID == "" {
		t.Error("expected gate_event_id to be set")
	}
}

func TestGateEvaluateHandler_Pass(t *testing.T) {
	handler := makeGateEvaluateHandler(&stubCommandments{})
	params, _ := json.Marshal(GateEvaluateParams{
		AgentID:  "claude-code",
		ToolName: "Read",
	})
	result, err := handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := result.(GateEvaluateResult)
	if res.Decision != "pass" {
		t.Fatalf("expected decision pass, got %q", res.Decision)
	}
}

func TestGateEvaluateHandler_TargetsMappedToArguments(t *testing.T) {
	// Verify targets are wired into AuditEvent.Arguments so argument-sensitive rules can fire.
	var capturedEvent *audit.AuditEvent
	svc := &capturingCommandments{fn: func(e *audit.AuditEvent) audit.EvalResult {
		capturedEvent = e
		return audit.EvalResult{}
	}}
	handler := makeGateEvaluateHandler(svc)
	params, _ := json.Marshal(GateEvaluateParams{
		AgentID:  "claude-code",
		ToolName: "Bash",
		Targets: GateTargets{
			Argv:     []string{"rm", "-rf", "/tmp/x"},
			Paths:    []string{"/tmp/x"},
			PathMode: "delete",
		},
	})
	_, err := handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedEvent == nil {
		t.Fatal("expected Evaluate to be called")
	}
	if capturedEvent.Arguments == "" {
		t.Fatal("expected Arguments to be populated from targets")
	}
}

// capturingCommandments is a CommandmentsService stub that calls a provided function.
type capturingCommandments struct {
	fn func(*audit.AuditEvent) audit.EvalResult
}

func (c *capturingCommandments) Evaluate(e *audit.AuditEvent) audit.EvalResult { return c.fn(e) }
func (c *capturingCommandments) Redact(*audit.AuditEvent, bool)                {}
func (c *capturingCommandments) List() []CommandmentRuleSummary                { return nil }
func (c *capturingCommandments) Test(e *audit.AuditEvent) audit.EvalResult     { return c.fn(e) }
func (c *capturingCommandments) Reload() (int, error)                          { return 0, nil }
```

**Step 2: Run to confirm failure**

```bash
eval "$(mise activate bash)" && go test ./internal/daemon/ -run TestGateEvaluateHandler -v
```

Expected: FAIL — types undefined.

**Step 3: Create `internal/daemon/gate.go`**

```go
package daemon

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/ipc"
)

// GateEvaluateParams is the JSON-RPC request schema for gate.evaluate.
type GateEvaluateParams struct {
	AgentID      string      `json:"agent_id"`
	ToolName     string      `json:"tool_name"`
	ToolCategory string      `json:"tool_category"`
	ToolEffect   string      `json:"tool_effect"`
	Targets      GateTargets `json:"targets,omitempty"`
}

// GateTargets mirrors the structured targets schema from the proxy ToolTargets.
type GateTargets struct {
	Argv     []string `json:"argv,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	PathMode string   `json:"path_mode,omitempty"`
}

// GateEvaluateResult is the JSON-RPC response for gate.evaluate.
type GateEvaluateResult struct {
	GateEventID   string `json:"gate_event_id"`
	Decision      string `json:"decision"` // "block" | "pass"
	CommandmentID string `json:"commandment_id,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Enforcement   string `json:"enforcement,omitempty"`
}

// makeGateEvaluateHandler returns an ipc.Handler for the gate.evaluate method.
func makeGateEvaluateHandler(svc CommandmentsService) ipc.Handler {
	return func(raw json.RawMessage) (interface{}, error) {
		var params GateEvaluateParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, err
		}

		eventID := uuid.NewString()

		// Map targets into Arguments so argument-sensitive commandment rules can fire.
		targetsJSON, _ := json.Marshal(map[string]interface{}{
			"argv":      params.Targets.Argv,
			"paths":     params.Targets.Paths,
			"path_mode": params.Targets.PathMode,
		})

		e := &audit.AuditEvent{
			ID:           eventID,
			Timestamp:    time.Now().UTC(),
			AgentID:      params.AgentID,
			ActionType:   audit.ActionToolCall,
			Action:       params.ToolName,
			ToolName:     params.ToolName,
			ToolCategory: params.ToolCategory,
			ToolEffect:   params.ToolEffect,
			Arguments:    string(targetsJSON),
			AdapterID:    "gate",
			AdapterType:  "gate",
		}

		result := svc.Evaluate(e)
		for _, triggered := range result.Triggered {
			if strings.EqualFold(triggered.Enforcement, "block") {
				e.Outcome = audit.OutcomeBlocked
				// Note: gate.evaluate emitter wiring is caller's responsibility (see RegisterGateEvaluate).
				// Blocked gate events must be emitted; pass-through events are not emitted.
				return GateEvaluateResult{
					GateEventID:   eventID,
					Decision:      "block",
					CommandmentID: triggered.Name,
					Reason:        triggered.Message,
					Enforcement:   "block",
				}, nil
			}
		}

		// Pass-through: no audit emission (by design — too high volume).
		return GateEvaluateResult{
			GateEventID: eventID,
			Decision:    "pass",
		}, nil
	}
}

// RegisterGateEvaluate wires the gate.evaluate handler onto the IPC server.
func RegisterGateEvaluate(srv *ipc.Server, svc CommandmentsService) {
	srv.Handle("gate.evaluate", makeGateEvaluateHandler(svc))
}
```

**Step 4: Wire in `daemon.go`**

Find where IPC handlers are registered in `daemon.go` (look for `ipcServer.Handle(` calls). Add after existing handlers:

```go
daemon.RegisterGateEvaluate(ipcServer, commandmentsService)
```

Note: you may need to import `daemon` package or restructure — the function is in the same package, so just call `RegisterGateEvaluate(ipcServer, d.commandments)` (verify the field name against `daemon.go`).

**Step 5: Run tests**

```bash
eval "$(mise activate bash)" && go test ./internal/daemon/ -run TestGateEvaluateHandler -v
```

Expected: both PASS.

**Step 6: Run full test suite**

```bash
eval "$(mise activate bash)" && go test -race ./...
```

Expected: PASS.

**Step 7: Commit**

```bash
git add internal/daemon/gate.go internal/daemon/gate_test.go internal/daemon/daemon.go
git commit -m "feat: add gate.evaluate IPC method for external PEP callers"
```

---

## End-to-End Verification

After all tasks complete, verify the full enforcement path with the existing E2E test:

```bash
eval "$(mise activate bash)" && go test -race -run TestDaemonProxyE2E ./...
```

And run benchmarks to confirm latency gates still hold:

```bash
eval "$(mise activate bash)" && make bench-gate
```

Expected: all gates pass (commandment eval p95 < 2ms, proxy roundtrip p95 < 20ms). Note: the streaming path now adds full response-generation latency for tool-call responses — this is expected and intentional.

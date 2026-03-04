# Local Enforcement Architecture Design

**Date:** 2026-03-03
**Status:** Revised — response-side enforcement as primary strategy

---

## Vision

**Crabwise enforces rules on AI agents by governing what instructions reach the agent — not by intercepting every possible thing the agent might do afterward.**

Every AI agent operates the same way: it sends a prompt to an LLM, the LLM responds with a decision (including any tool calls it wants to make), and the agent acts on that response. Crabwise sits in that loop. The LLM response is the moment of instruction — the point where "run `rm -rf /`" moves from the model's weights into an actionable command. That is the universal enforcement boundary, and Crabwise already owns it via the HTTP proxy.

The primary enforcement strategy is: **evaluate LLM responses before they reach the agent and block harmful tool instructions at the source.** If the agent never receives the instruction, it never executes it. No agent modification. No hooks. No OS-level complexity. Works for every agent that routes through the proxy to a supported provider.

---

## 1. The Problem

### What works today

The HTTP proxy intercepts LLM API calls and enforces commandments on the **request side** — it can block a prompt from reaching the LLM if it violates policy. This works for any agent using the proxy and requires no agent modification.

### The gap

The proxy does not currently evaluate LLM **responses**. When the LLM responds with tool calls (e.g., "run bash command `rm -rf /tmp/project`"), the proxy forwards them to the agent unexamined. The agent then executes those tool calls locally.

By the time the action happens:
- It is in-process — the proxy has no visibility
- The logwatcher sees it in JSONL — after execution, too late to block

A commandment like `"never run rm -rf"` can warn retroactively but cannot prevent the deletion.

### The scope of the gap

| What the proxy sees today | Enforcement |
|---|---|
| Prompt sent to LLM | Request-side: can block before LLM sees it |
| LLM response with tool_use blocks | **Not evaluated — forwarded as-is** |
| Local tool execution (bash, write, etc.) | Never visible — no enforcement possible |

---

## 2. The Solution: Response-Side Enforcement

### The insight

Every agent receives its tool instructions via the LLM response. The proxy is already in that path — it reads and buffers LLM responses for normalization and token counting. It just doesn't evaluate the tool calls *inside* those responses.

Extending the proxy to evaluate `tool_use` blocks in LLM responses before forwarding them gives Crabwise **semantic enforcement for local tools across all supported providers** — without touching a single agent, hook, or OS primitive.

```
Agent  ──── prompt ────►  [PROXY]  ──► LLM API
                                            │
                              LLM responds: │
                   tool_use: bash("rm -rf /")
                                            │
                          [PROXY] evaluates ◄
                          commandments on
                          each tool_use
                                  │
                    block?  ──────┘
                      │
             ┌────────┴──────────┐
             │ YES               │ NO
             ▼                   ▼
     Return error to     Forward response
     agent. Tool never   to agent. Agent
     reaches agent.      executes normally.
```

The agent either receives a clean response or an error. It never receives an instruction it is not permitted to act on.

### Why this is the right primary strategy

- **Provider-scoped** — any agent routing through the proxy to a supported provider gets enforcement, regardless of agent architecture
- **Plug-and-play** — no agent modification, no hooks, no OS primitives
- **Semantic** — evaluates the actual tool intent as the LLM expressed it, with full argument structure
- **The proxy already buffers responses** — the infrastructure exists; this is an evaluation pass, not a new pipeline
- **Enforcement at the source** — prevents the harmful instruction from ever existing in the agent's execution context

---

## 3. Architecture

### Enforcement flow

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP PROXY (PEP)                       │
│                                                             │
│  Request path (existing):                                   │
│    prompt → normalize → evaluate prompt → forward or block  │
│                                                             │
│  Response path (new):                                       │
│    LLM response → parse tool_use blocks                     │
│                 → evaluate each tool_use (commandment eng.) │
│                 → forward clean response OR return error    │
│                                                             │
└──────────────────────────────┬──────────────────────────────┘
                               │ evaluates via
                               ▼
              ┌─────────────────────────────┐
              │     COMMANDMENT ENGINE      │
              │     (PDP — unchanged)       │
              │                             │
              │  Same engine that evaluates │
              │  request-side today         │
              └─────────────────────────────┘
```

The commandment engine does not change. The proxy calls it on tool_use blocks extracted from the response, the same way it currently calls it on tool definitions in the request. The enforcement decision is: block (a commandment matched) or pass through (no commandment matched).

### What "block" means on the response side

When a tool_use block violates a commandment, the proxy has two options:

| Mode | Behavior | When to use |
|---|---|---|
| **Block response** | Return an error to the agent (e.g., HTTP 403 with structured reason) | Safest; agent receives no partial response |
| **Redact tool_use** | Strip or replace the violating tool_use block, forward the rest | Allows agent to continue without the blocked action; more complex |

Block-response is the default. Redact-tool_use is a future refinement for cases where the response contains both permitted and blocked tool calls.

### Streaming responses

LLM responses are often streamed via SSE. Tool_use blocks arrive as fragmented delta events that must be reassembled before evaluation. This imposes a buffering requirement with a specific latency contract.

**v1 contract — full pre-buffer:**

The proxy buffers the entire streaming response body before forwarding anything to the agent. Once the stream ends (or a tool_use block is fully reconstructed), the proxy evaluates all tool_use blocks:
- If all allowed: forward the buffered response body to the agent (as a complete response, not re-streamed)
- If any blocked: discard the buffer, return HTTP 403

This guarantees clean HTTP semantics — the agent receives either a complete valid response or an unambiguous error. The latency cost is full response generation time for any response containing tool calls.

**Why tool-call arguments arrive in fragments (provider-specific):**

| Provider | Tool-use signal in stream | Argument delivery |
|---|---|---|
| OpenAI-compatible | `delta.tool_calls[].function.name` in first chunk | Arguments accumulate as string fragments across subsequent chunks; complete at `finish_reason: "tool_calls"` |
| Anthropic native | `content_block_start` with `type: tool_use` | Arguments accumulate via `content_block_delta` with `input_json_delta`; complete at `content_block_stop` |

Text content cannot be safely forwarded before the tool_use decision because the proxy cannot know whether a tool_use block will appear later in the stream. Once bytes are sent with HTTP 200, a subsequent 403 is not possible.

**Buffer size limit:** If the response body exceeds a configurable maximum (default 10 MB), the proxy fails closed and returns an error. This is not a policy decision — it is a system/enforcement error. The error response distinguishes these two error classes:

| Situation | HTTP status | `error.type` | Agent interpretation |
|---|---|---|---|
| Commandment matched — tool blocked | 403 | `policy_violation` | Do not retry; log and surface to user |
| Buffer overflow or parse failure | 502 | `enforcement_error` | May retry; not a commandment match |

Agents must not treat `enforcement_error` as evidence of a policy violation. Tooling and UI must surface these separately.

**Future optimization:** Lazy buffering — stream text deltas immediately, buffer only when a tool_use signal is detected, abort the connection on block. This requires the agent to handle a mid-stream connection close as an enforcement event. Not part of v1.

The proxy already has streaming infrastructure (`proxySSEStream`). Response-side enforcement replaces its pass-through behavior with the pre-buffer strategy described above.

---

## 4. What the Proxy Evaluates

### Request side (existing)

The proxy evaluates the prompt and tool definitions before forwarding to the LLM. Blocks if the request itself violates policy (e.g., prompt contains sensitive data, dangerous tools are defined).

### Response side (new)

The proxy evaluates each `tool_use` block in the LLM response before forwarding. The commandment engine evaluation interface (`Evaluate(*audit.AuditEvent)`) is unchanged — the engine consumes the same `AuditEvent` schema. What changes is the proxy's response processing layer, which must:

1. **Extract** `tool_use` blocks from the LLM response (provider-specific parsing)
2. **Classify** tool intent using the existing classifier (same path as request-side tool definitions)
3. **Normalize** tool_input arguments into structured targets

For each extracted tool call:

| Field | Source | Use |
|---|---|---|
| `tool_name` | LLM response `tool_use.name` | e.g., `"Bash"`, `"Write"`, `"computer"` |
| `tool_category` | Classifier (existing) | e.g., `"shell"`, `"file_write"` |
| `tool_effect` | Classifier (existing) | e.g., `"write"`, `"delete"` |
| `tool_input` | LLM response `tool_use.input` | Raw args as the LLM structured them |
| `targets.paths` | Parsed from `tool_input` | File paths the tool will affect |
| `targets.argv` | Parsed from `tool_input` | Shell command argv (for bash tools) |

Steps 1–3 are new proxy-layer code. The `AuditEvent` produced by this normalization flows into `evaluator.Evaluate()` unchanged. This is richer than request-side evaluation: the proxy sees not just "this agent has the bash tool" but "this agent is about to run `rm -rf /tmp/project` with this specific argv."

The structured targets schema (step 3) requires per-tool-name argument parsers. These live in the proxy, not the engine. A `Bash` tool parser extracts `argv` from the `command` field; a `Write` tool parser extracts paths from the `path` field, etc.

---

## 5. Agent Compatibility

**Any agent that routes through the Crabwise proxy to a supported provider gets response-side enforcement automatically.** No agent modification, no install step beyond pointing the agent at the proxy.

| Agent | Proxy-based local enforcement | How |
|---|---|---|
| Claude Code | Yes (v1) | `HTTPS_PROXY` + Crabwise CA cert. One-time setup. Uses OpenAI-compatible transport. |
| Codex CLI | Yes (v1) | Same. |
| OpenClaw | Yes (v1) | Same. |
| Cursor | Yes (v1) | Same. |
| Any OpenAI-compatible client | Yes (v1) | Same. |
| Anthropic-native clients | Planned (v2) | Requires Anthropic transport with `content_block_*` event parsing. |

This is the key property of the proxy enforcement model: enforcement is in the proxy, not the agent.

### Provider scope for v1

Response-side enforcement requires provider-specific tool-call extraction. "Universal" means universal within supported providers, not all possible LLM APIs.

| Provider | Streaming tool-use format | v1 support |
|---|---|---|
| OpenAI / OpenAI-compatible | `delta.tool_calls[].function.{name,arguments}` fragments | Yes |
| Anthropic native | `content_block_start/delta/stop` with `type: tool_use` | v2 |
| Gemini | `candidates[].content.parts[].functionCall` objects | v2+ |

Each provider requires a corresponding `Transport` implementation that knows how to reconstruct complete tool_use intents from that provider's streaming format. The enforcement contract — evaluate before forward — is provider-agnostic. The parsing is not.

### What the proxy cannot cover

The proxy governs what tool instructions **reach** the agent. It cannot govern tool executions the agent initiates without an LLM instruction (e.g., a scripted action, a retry on cached instructions, or a jailbreak where the agent ignores the blocked response and proceeds from memory).

For those cases, secondary enforcement exists (see §7). But the primary surface — the LLM response — covers the overwhelming majority of real agent behavior.

---

## 6. PEP/PDP Architecture

Crabwise uses a **Policy Decision Point / Policy Enforcement Point** (PDP/PEP) model:

- **PDP** — the commandment engine in the Crabwise daemon. Single source of truth. Unchanged.
- **PEP** — sits at a boundary, calls the PDP, enforces the decision.

The proxy is both a PEP (it enforces) and owns the boundary (the LLM API response channel). This is the universal PEP.

```
PEPs by boundary:

  LLM response (universal boundary)  →  Proxy response-side eval  [primary — supported providers]
  LLM request               →  Proxy request-side eval   [existing — all agents]
  Per-agent hooks           →  Claude Code PreToolUse     [secondary — CC only]
  OS process boundary       →  Platform containment       [coarse safety net]
```

### `gate.evaluate` IPC method

The daemon exposes `gate.evaluate` as a JSON-RPC method for any PEP to call. The proxy calls it internally (as it already calls `shouldBlock()`). Agent-side hooks (e.g., Claude Code `PreToolUse`) can call it externally via the Unix socket for additional depth.

```json
// Request — same schema whether called by proxy or by agent hook
{
  "method": "gate.evaluate",
  "params": {
    "agent_id": "claude-code",
    "tool_name": "Bash",
    "tool_category": "shell",
    "tool_effect": "write",
    "targets": {
      "argv": ["rm", "-rf", "/tmp/project"],
      "paths": ["/tmp/project"],
      "path_mode": "delete"
    }
  }
}

// Response
{
  "result": {
    "gate_event_id": "evt-uuid",
    "decision": "block",
    "commandment_id": "no-rm-rf",
    "reason": "destructive delete blocked",
    "enforcement": "block"
  }
}
```

---

## 7. Secondary Enforcement: Per-Agent Hooks

Response-side enforcement blocks instructions before they reach the agent. For defense in depth — or for cases where an agent might act without an LLM instruction — per-agent hooks provide a second enforcement layer at the agent's tool-dispatch boundary.

**This is not the primary strategy.** It is supplementary.

| Agent | Hook mechanism | Coverage | Setup |
|---|---|---|---|
| Claude Code | `PreToolUse` hook in `~/.claude/settings.json` | All CC tools | `crabwise install --agent claude-code` writes the hook once |
| Others | None available today | N/A | Requires vendor to expose pre-exec hook |

For Claude Code, the hook calls `gate.evaluate` externally via the Unix socket before each tool. Combined with response-side enforcement, Claude Code gets double-gated: Crabwise blocks the LLM instruction before it arrives, and blocks the tool dispatch if anything slips through.

For all other agents: response-side proxy enforcement is the only plug-and-play enforcement layer available today.

---

## 8. Safety Net: OS-Level Containment

For agents where neither proxy enforcement nor hooks are sufficient (e.g., a compromised agent operating from cached instructions), `crabwise run <agent>` wraps the agent in OS-level process constraints.

This is a coarse safety net, not a primary enforcement mechanism. It operates at the syscall level with no semantic visibility into what the agent is doing or why.

| Platform | Mechanism |
|---|---|
| Linux | seccomp-BPF (syscall filter), or eBPF LSM hooks |
| macOS | Sandbox profiles, or Endpoint Security (requires entitlement) |

---

## 9. What Changes

### Primary change: proxy response-side evaluation

| Item | Change |
|---|---|
| `proxy.handleProxy` | After receiving LLM response, extract and evaluate `tool_use` blocks before forwarding |
| `proxy.proxySSEStream` | Replace pass-through with pre-buffer strategy; forward only after tool_use evaluation |
| `Transport` interface | Add `ExtractToolUseBlocks(body []byte) ([]ToolUseBlock, error)` for provider-specific parsing |
| Proxy response normalization | New: parse tool_input arguments into structured targets (argv, paths, etc.) for each tool_use |
| Commandment engine | Unchanged — proxy calls `evaluator.Evaluate()` with AuditEvents constructed from tool_use blocks |
| Audit pipeline | Blocked tool_use always emits `AuditEvent` with `AdapterType: proxy`, `Outcome: blocked`. Passed-through tool_use does not emit by default (too high volume — one per LLM call). |

### Secondary change: `gate.evaluate` IPC method

| Item | Change |
|---|---|
| IPC server | Add `gate.evaluate` JSON-RPC method for external callers (agent hooks) |
| Claude Code hook | `crabwise install --agent claude-code` writes `PreToolUse` hook to `~/.claude/settings.json` |

### What doesn't change

| Item | Status |
|---|---|
| Commandment engine | Unchanged |
| Existing request-side enforcement | Unchanged |
| Audit event pipeline | Unchanged |
| All existing tests | Unchanged |
| `CanEnforce()` on `Adapter` | Deprecate — enforcement is a proxy/PEP concern, not an adapter property |

---

## 10. Audit

### Blocked tool_use in LLM response

When the proxy blocks a tool_use from an LLM response:

| Field | Value |
|---|---|
| `ID` | New UUID |
| `ActionType` | Tool category (e.g., `shell`, `file_write`) |
| `Outcome` | `blocked` |
| `AdapterType` | `proxy` |
| `Arguments` | Tool name, structured targets, commandment ID, reason |

The agent receives no instruction and executes no tool, so there is no post-hoc logwatcher event. The proxy audit event is the complete and authoritative record.

### Passed-through tool_use

When no commandment blocks a tool_use, the proxy forwards the response without emitting an audit event. Emitting an event for every passed-through tool_use would generate one enforcement-layer event per LLM API call — too high volume for practical audit use.

If the agent subsequently executes the tool and writes JSONL, the logwatcher emits its own event (existing behavior). That logwatcher event is the observable record of the passed-through tool execution.

**Design position:** the enforcement audit record consists of `blocked` events from the proxy. The logwatcher provides post-hoc observation of tool execution for allowed operations. These are separate concerns. The proxy does not attempt to correlate its records with logwatcher records — no agent modification means no reliable correlation handle.

---

## 11. Unresolved Questions

1. Block-entire-response vs. redact-blocked-tool_use: decided — block-entire for v1; redact is a future mode.
2. Streaming buffer max size: default 10 MB proposed. Specific value TBD based on observed response sizes.
3. Audit emit on pass-through: decided — blocked events only; passed-through tool_use does not emit.
4. Should `CanEnforce()` be removed from `Adapter` now or deprecated with a notice?
5. Per-tool-name argument parsers (§4): what tools need parsers in v1? (At minimum: `Bash`/`computer` for argv, `Write`/`Edit` for paths.)

# Local Enforcement Architecture Design

**Date:** 2026-03-03
**Status:** Revised — response-side enforcement as primary strategy

---

## Vision

**Crabwise enforces rules on AI agents by governing what instructions reach the agent — not by intercepting every possible thing the agent might do afterward.**

Every AI agent operates the same way: it sends a prompt to an LLM, the LLM responds with a decision (including any tool calls it wants to make), and the agent acts on that response. Crabwise sits in that loop. The LLM response is the moment of instruction — the point where "run `rm -rf /`" moves from the model's weights into an actionable command. That is the universal enforcement boundary, and Crabwise already owns it via the HTTP proxy.

The primary enforcement strategy is: **evaluate LLM responses before they reach the agent and block harmful tool instructions at the source.** If the agent never receives the instruction, it never executes it. No agent modification. No hooks. No OS-level complexity. Works for every agent that routes through the proxy.

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

Extending the proxy to evaluate `tool_use` blocks in LLM responses before forwarding them gives Crabwise **universal semantic enforcement for local tools** — without touching a single agent, hook, or OS primitive.

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

- **Universal** — any agent that routes through the proxy gets enforcement, regardless of architecture
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

The commandment engine does not change. The proxy calls it on tool_use blocks extracted from the response, the same way it currently calls it on tool definitions in the request. The enforcement decision is the same: allow or block.

### What "block" means on the response side

When a tool_use block violates a commandment, the proxy has two options:

| Mode | Behavior | When to use |
|---|---|---|
| **Block response** | Return an error to the agent (e.g., HTTP 403 with structured reason) | Safest; agent receives no partial response |
| **Redact tool_use** | Strip or replace the violating tool_use block, forward the rest | Allows agent to continue without the blocked action; more complex |

Block-response is the default. Redact-tool_use is a future refinement for cases where the response contains both permitted and blocked tool calls.

### Streaming responses

LLM responses are often streamed via SSE. Evaluating tool_use blocks requires buffering enough of the stream to reconstruct them before forwarding.

**Strategy:**
- Buffer stream until tool_use blocks are complete (they arrive in full JSON chunks in most providers)
- Evaluate buffered tool_use blocks against commandments
- If clean: begin forwarding buffered content, then stream remainder in real-time
- If blocked: discard buffer, return error

**Latency impact:** Added delay equals time-to-first-tool-use-block in the stream, typically less than full response generation time. Text content before any tool call is buffered but not delayed to the user since we evaluate the tool_use when it appears.

The proxy already has streaming infrastructure (`proxySSEStream`). This extends it with an evaluation pass.

---

## 4. What the Proxy Evaluates

### Request side (existing)

The proxy evaluates the prompt and tool definitions before forwarding to the LLM. Blocks if the request itself violates policy (e.g., prompt contains sensitive data, dangerous tools are defined).

### Response side (new)

The proxy evaluates each `tool_use` block in the LLM response before forwarding. For each tool call:

| Field | Source | Use |
|---|---|---|
| `tool_name` | LLM response | e.g., `"Bash"`, `"Write"`, `"computer"` |
| `tool_category` | Classifier (existing) | e.g., `"shell"`, `"file_write"` |
| `tool_effect` | Classifier (existing) | e.g., `"write"`, `"delete"` |
| `arguments` | LLM response | Raw args as the LLM structured them |
| `targets.paths` | Parsed from args | File paths the tool will affect |
| `targets.argv` | Parsed from args | Shell command argv (for bash tools) |

This is richer than request-side evaluation: the proxy sees not just "this agent has the bash tool" but "this agent is about to run `rm -rf /tmp/project` with this specific argv."

---

## 5. Agent Compatibility

**Any agent that routes through the Crabwise proxy gets response-side enforcement automatically.** No agent modification, no install step beyond pointing the agent at the proxy.

| Agent | Proxy-based local enforcement | How |
|---|---|---|
| Claude Code | Yes | `HTTPS_PROXY` + Crabwise CA cert. One-time setup. |
| Codex CLI | Yes | Same. |
| OpenClaw | Yes | Same. |
| Cursor | Yes | Same. |
| Any LLM API client | Yes | Same. |

This is the key property of the proxy enforcement model: it is agent-agnostic by design.

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

  LLM response (universal)  →  Proxy response-side eval  [primary — all agents]
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
| `proxy.handleProxy` | After receiving LLM response, parse and evaluate `tool_use` blocks before forwarding |
| `proxy.proxySSEStream` | Buffer stream to identify tool_use blocks; evaluate before forwarding chunk |
| Commandment engine | Unchanged — proxy calls same evaluation path for response-side tool_use |
| Audit pipeline | Blocked tool_use in response emits `AuditEvent` with `AdapterType: proxy`, `Outcome: blocked` |

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

## 10. Audit and Correlation

### Blocked tool_use in LLM response

When the proxy blocks a tool_use from an LLM response:

| Field | Value |
|---|---|
| `ID` | New UUID (correlation handle) |
| `ActionType` | Tool category (e.g., `shell`, `file_write`) |
| `Outcome` | `blocked` |
| `AdapterType` | `proxy` |
| `Arguments` | Includes tool name, structured targets, commandment ID, reason |

The agent receives no instruction and executes no tool, so there is no post-hoc logwatcher event to correlate. The audit event is the complete record.

### Allowed tool_use

When a tool_use is allowed through the proxy and the agent executes it, two events exist:
1. Proxy response evaluation: `gate_event_id` assigned, `Outcome: allowed`
2. Logwatcher post-hoc event (if agent writes JSONL)

Correlation: the proxy embeds `gate_event_id` in a response header (`X-Crabwise-Gate-Event-ID`) that the agent can optionally include in its JSONL for linkage. Without it, the two events are independent observations.

---

## 11. Unresolved Questions

1. Should the proxy block the entire response when any tool_use is blocked, or attempt to redact just the blocked tool_use and forward the rest? (Default: block entire response.)
2. For streaming responses: what is the maximum acceptable buffer size before the proxy must make a decision and either forward or abort?
3. Should `gate.evaluate` emit audit events for `allow` decisions, or only `block`? (Volume vs. completeness tradeoff.)
4. Should `CanEnforce()` be removed from `Adapter` now or deprecated with a notice?

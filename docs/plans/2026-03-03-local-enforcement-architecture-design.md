# Local Enforcement Architecture Design

**Date:** 2026-03-03
**Status:** Draft — for review

---

## 1. Problem Statement

Crabwise can monitor AI agents and block LLM API calls via the HTTP proxy adapter. However, it **cannot prevent local command execution** — actions like running a bash command, writing a file, reading a file, or editing code that AI agents perform directly on the host machine without touching the network.

### Why this matters

When Claude Code (or any AI coding agent) executes a local tool:

1. The action is performed **in-process** — no HTTP call goes through the proxy.
2. The logwatcher eventually sees it in the JSONL log — **after** the action already happened.
3. Commandments can warn retroactively, but **block is meaningless** for a completed action.

This means a commandment like `"never run rm -rf"` can log a warning but cannot stop the deletion. Any enforcement model that cannot act before local tool execution is fundamentally incomplete.

### The scope of the gap

| Adapter | What it sees | Enforcement timing |
|---|---|---|
| logwatcher | JSONL post-hoc log | After execution — too late |
| HTTP proxy | LLM API request/response | Before network call — can block |
| openclaw | Governance gateway events | After decision — too late |
| **[missing]** | Local tool execution | Before exec — **no PEP exists** |

The proxy fills this gap for remote API calls. Nothing fills it for local actions.

---

## 2. Root Cause: One PEP Flavor, Multiple Boundaries

The current architecture has one enforcement pattern: **sit in the network path and intercept HTTP**. This works only for traffic that flows through a proxy. Local tool execution does not.

The crabwise daemon already acts as a **Policy Decision Point (PDP)** — it holds commandments and evaluates events. What is missing is a **Policy Enforcement Point (PEP)** at the local tool execution boundary.

A PEP/PDP architecture:
- **PDP** — centralized policy engine; evaluates candidate actions against rules; returns allow/deny + rationale
- **PEP** — sits at a boundary where actions can be intercepted; calls the PDP before letting the action proceed

The proxy is a network PEP. The local execution boundary has no PEP. The fix is to add one — and to define a stable protocol so future adapters can add more.

---

## 3. Recommended Architecture: Single PDP, Multiple PEP Flavors

### Core principle

Keep the commandment engine + daemon as the **single PDP**. Define a **stable enforcement protocol** that any PEP can call. Implement multiple PEP flavors specialized to different boundaries.

```
                         ┌─────────────────────────────────┐
                         │        CRABWISE DAEMON          │
                         │                                 │
                         │  ┌─────────────────────────┐   │
  Any PEP  ──gate.eval──►│  │   Commandment Engine    │   │
                         │  │   (PDP — single source  │   │
  ◄── {allow|block} ─────│  │    of truth)            │   │
                         │  └─────────────────────────┘   │
                         │                                 │
                         │  Audit event pipeline           │
                         │  IPC server (JSON-RPC 2.0)      │
                         └─────────────────────────────────┘

PEP flavors:
  Network proxy PEP     — intercepts LLM API calls (exists)
  Local tool shim PEP   — wraps bash/shell/etc. (new)
  OS sidecar PEP        — coarse syscall containment (future safety net)
  In-process library    — embedded PEP for cooperative agents (future)
```

### Enforcement protocol

The daemon exposes enforcement as IPC methods. Any PEP calls these to get a decision:

| Method | Purpose | Status |
|---|---|---|
| `gate.evaluate` | Evaluate a single candidate action; returns allow/block | New |
| `gate.evaluate_plan` | Evaluate a sequence of actions before execution | Future |
| `gate.issue_capability` | Issue a time-bound, scope-limited capability token | Future |

`gate.evaluate` is the first method to implement. It is the synchronous equivalent of the proxy's internal `shouldBlock()` call, now accessible to any PEP via the Unix socket.

#### `gate.evaluate` request/response

```json
// Request
{
  "jsonrpc": "2.0",
  "method": "gate.evaluate",
  "id": "1",
  "params": {
    "agent_id": "claude-code",
    "session_id": "abc123",
    "tool_name": "Bash",
    "tool_category": "shell",
    "tool_effect": "write",
    "arguments": "rm -rf /tmp/project",
    "working_dir": "/home/user/project",
    "timestamp": "2026-03-03T12:00:00Z"
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": "1",
  "result": {
    "decision": "block",         // "allow" | "block"
    "commandment_id": "no-rm-rf",
    "reason": "matches rule: never run destructive delete",
    "enforcement": "block"       // "warn" | "block"
  }
}
```

### Adapter contract: normalize actions, not transports

The key insight from prior taxonomy work applies here directly:

- **Adapters** translate provider-specific events (JSONL, HTTP payloads, gateway events) into the **normalized action schema** (tool category, effect, resource path, risk level).
- **PEPs** capture candidate actions at a boundary and package them into that schema before calling `gate.evaluate`.
- **The PDP** evaluates against commandments and returns a decision.

This means new adapters never change enforcement logic. They only add:
1. A translation layer (provider format → normalized schema)
2. A PEP that calls `gate.evaluate` at the right moment

---

## 4. PEP Implementation Tiers

### Tier 1 — Semantic PEPs (implement first)

#### Network proxy PEP (exists)
HTTP MITM proxy intercepts all LLM API calls. Calls the commandment engine (`shouldBlock()`) synchronously before forwarding. Can block at network layer.

**Gap:** Only covers network traffic. Local tools invisible.

#### Local tool shim PEP (new — primary gap closure)

Ship a `crab-shell` binary that wraps `/bin/bash` (and optionally other high-risk tools). Before executing any command:

1. `crab-shell` serializes the intent: agent ID, command, working dir, tool category.
2. Calls `gate.evaluate` via the Unix socket.
3. If `allow`: execs the real shell.
4. If `block`: exits with structured JSON error and non-zero exit code.

Agent integration: configure the agent to call `crab-shell` instead of `/bin/bash`. For Claude Code, this is a `PreToolUse` hook or a `bash` path override in settings.

**Pros:**
- OS-portable (just a process boundary — no kernel hacks)
- Semantic (knows tool category, not raw syscall)
- Reuses existing commandment engine unchanged
- Claude Code hook mechanism already exists

**Cons:**
- Requires agent to support configurable shell/tool path
- Does not catch syscalls made outside the tool boundary (covered by Tier 3 safety net)

#### In-process library PEP (for cooperative open-source agents)

For agents where crabwise has deep integration (e.g., OpenClaw), embed a lightweight PDP client as a library. Library makes a function call (not a Unix socket round-trip) to evaluate actions. Internally may still route to daemon or use a shared-memory decision cache.

**When to use:** Performance-critical agents where 1–2ms IPC latency is unacceptable. Requires agent cooperation and SDK linking.

---

### Tier 2 — Planning-level governance (future)

For agents that can surface multi-step plans before execution, `gate.evaluate_plan` evaluates the full sequence. Reject dangerous plans early (e.g., "any step touching `/etc/`"). Approve with constraints. Reduces per-call IPC overhead for approved plans.

---

### Tier 3 — OS sidecar (safety net, not primary enforcement)

For uncooperative or closed-source agents with no hook mechanism: a sidecar process attaches via OS-level tracing (Linux: seccomp-BPF + eBPF; macOS: Endpoint Security). The sidecar:

- Applies coarse hard limits (allowed directories, blocked syscalls) without calling the daemon.
- For ambiguous actions, calls `gate.evaluate` with a reconstructed semantic event.

**This is a safety net, not a primary enforcement mechanism.** Classic pitfalls apply: TOCTOU races, argument reconstruction imprecision, platform specificity. Deploy beneath Tier 1 PEPs, not instead of them.

---

### Tier 4 — Configuration provisioning (for closed platforms)

For SaaS-style agents with their own native policy controls (e.g., a managed Cursor or Copilot environment), crabwise acts as the source-of-truth and **pushes** desired policy configuration into the agent's own settings. No inline PEP needed; enforcement happens inside the platform.

**Limitation:** Coverage is bounded by whatever knobs the platform exposes.

---

## 5. What Changes vs. What Doesn't

### What changes

| Item | Change |
|---|---|
| IPC server | Add `gate.evaluate` JSON-RPC method |
| New binary: `crab-shell` | Thin tool shim PEP wrapping `/bin/bash` |
| `CanEnforce()` on `Adapter` | Deprecate or replace with PEP taxonomy (adapters are observers or translators; PEPs are the enforcement layer) |
| Docs | Clarify PEP/PDP model for contributors adding new adapters |

### What doesn't change

| Item | Status |
|---|---|
| Commandment engine | Unchanged — `gate.evaluate` just calls it |
| Audit event pipeline | Unchanged — blocked events still emit to audit log |
| Existing adapter interface | Unchanged for event emission |
| Proxy enforcement | Unchanged — continues to call `shouldBlock()` internally |
| All existing tests | Unchanged |

---

## 6. Audit Events for Gate Decisions

When `gate.evaluate` returns `block`, the daemon emits an `AuditEvent` with:
- `ActionType`: the tool category from the request
- `Outcome`: `blocked`
- `AdapterType`: `gate`
- `Arguments`: includes tool name, command, decision rationale, commandment ID

This ensures blocked local commands appear in the audit trail identically to blocked API calls. The logwatcher may also see the same event from the post-hoc JSONL — deduplication by event fingerprint prevents double-counting.

---

## 7. Unresolved Questions

1. How does `crab-shell` identify the calling agent? (PID-based, env var, or socket credential?)
2. What's the failure mode when the daemon is unreachable — fail open or fail closed? Configurable per commandment severity?
3. Does `gate.evaluate` also emit an audit event for allowed actions, or only for blocks?
4. Should `CanEnforce()` be removed now or kept for backwards compat with any external consumers?
5. Is `gate.evaluate_plan` needed before `crab-shell`, or is per-call evaluation sufficient for Tier 1?

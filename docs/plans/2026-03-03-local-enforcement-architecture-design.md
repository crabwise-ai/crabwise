# Local Enforcement Architecture Design

**Date:** 2026-03-03
**Status:** Draft — revised after engineering review

---

## 1. Problem Statement

Crabwise can monitor AI agents and block LLM API calls via the HTTP proxy adapter. However, it **cannot prevent local tool execution** — actions like running a bash command, writing a file, reading a file, or editing code that AI agents perform directly on the host machine without touching the network.

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

The current architecture has one enforcement pattern: sit in the network path and intercept HTTP. This works only for traffic that flows through a proxy. Local tool execution does not.

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
  Network proxy PEP          — intercepts LLM API calls (exists)
  Tool invocation PEP        — generic hook at agent tool boundary (new)
    └─ shell adapter         — wraps /bin/bash as one implementation
    └─ file-write adapter    — wraps write/edit tools as another
    └─ ... (per tool family)
  Platform containment       — OS-specific coarse safety net (future)
  In-process library         — embedded PEP for cooperative agents (future)
```

### Enforcement protocol

The daemon exposes enforcement as IPC methods:

| Method | Purpose | Status |
|---|---|---|
| `gate.evaluate` | Evaluate a single candidate action; returns allow/block + correlation ID | New |

The execution lifecycle near-term protocol (`gate.evaluate` is the first step):

1. **Pre-exec:** PEP calls `gate.evaluate` → daemon returns decision + `gate_event_id`
2. **Execution:** PEP either blocks (exits non-zero + JSON error) or proceeds (execs the tool)
3. **Post-exec report:** PEP optionally calls `gate.report` with outcome + `gate_event_id` (future)
4. **Correlation:** `gate_event_id` is the stable handle linking pre-exec decision to any post-hoc observations

Plan evaluation and capability tokens are longer-term extensions built on top of this lifecycle, not near-term methods to define now.

#### `gate.evaluate` request/response

```json
// Request
{
  "jsonrpc": "2.0",
  "method": "gate.evaluate",
  "id": "1",
  "params": {
    // Identity — advisory. Authoritative identity is SO_PEERCRED (see §4).
    "agent_id": "claude-code",
    "session_id": "abc123",

    // Tool intent — structured, not a raw string
    "tool_name": "Bash",
    "tool_category": "shell",
    "tool_effect": "write",
    "working_dir": "/home/user/project",
    "timestamp": "2026-03-03T12:00:00Z",

    // Structured targets — typed fields, not free-text arguments
    "targets": {
      "argv": ["rm", "-rf", "/tmp/project"],   // shell: structured argv
      "paths": ["/tmp/project"],               // file tools: affected paths
      "path_mode": "delete",                   // read | write | delete | exec
      "network_destination": null,             // net tools: host:port
      "env_delta": {}                          // env var changes, if relevant
    }
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": "1",
  "result": {
    "gate_event_id": "evt-uuid-here",  // correlation handle for lifecycle
    "decision": "block",               // "allow" | "block"
    "commandment_id": "no-rm-rf",
    "reason": "matches rule: never run destructive delete",
    "enforcement": "block"             // "warn" | "block"
  }
}
```

### Shell enforcement position

Shell commands present a special challenge: a raw command string is not a stable security object. Shell parsing, globbing, variable expansion, command substitution, and chained commands all transform the string before execution, making string-match rules trivially bypassable.

The architecture takes this explicit position:

- **For cooperative agents with hook support (e.g., Claude Code `PreToolUse`):** enforce at the agent's tool-intent boundary, before shell interpretation. The agent sends structured intent (`{"tool": "Bash", "args": {"command": "rm -rf /tmp"}}`). This is the authoritative form — the agent has decided what to run before the shell sees it. PEP evaluates this structured intent.
- **For shell-wrapping PEPs (e.g., `crab-shell`):** receive structured `argv` via the OS (not shell-expanded strings). Rules operating on raw shell strings are explicitly **best-effort only** and documented as such.
- Policies that require semantic accuracy MUST target structured fields (`targets.argv`, `targets.paths`, etc.), not `arguments` free-text.

### Adapter contract: normalize actions, not transports

- **Adapters** translate provider-specific events (JSONL, HTTP payloads, gateway events) into the **normalized action schema** defined here.
- **PEPs** capture candidate actions at a boundary and package them into that schema before calling `gate.evaluate`.
- **The PDP** evaluates against commandments and returns a decision.

New adapters never change enforcement logic. They add only:
1. A translation layer (provider format → normalized schema with structured `targets`)
2. A PEP that calls `gate.evaluate` at the right boundary

---

## 4. Identity Model

`gate.evaluate` receives `agent_id` and `session_id` in the request body. These fields are **advisory** — they are used for audit display and grouping but cannot be trusted for policy evaluation by themselves.

**Authoritative identity** comes from the OS, not the request body:

| Mechanism | What it provides | Trust level |
|---|---|---|
| `SO_PEERCRED` on Unix socket | UID + PID of the calling process | Authoritative |
| Daemon-launched agent | Daemon knows the PID it spawned → maps to session | Authoritative |
| Externally-launched agent | UID is authoritative; `session_id` is advisory | Partial |

`SO_PEERCRED` is already used for IPC auth in the existing server (`internal/ipc/server.go`). `gate.evaluate` extends the same mechanism: the daemon reads UID/PID from SO_PEERCRED on the socket connection and uses this as the authoritative caller identity.

Session binding for externally-launched agents (e.g., a user started Claude Code independently and configured it to call `gate.evaluate`) is best-effort: UID ties the caller to a user, `session_id` in the request is accepted as advisory for display but does not grant any elevated permissions.

---

## 5. Failure Semantics

When the daemon is unreachable or `gate.evaluate` times out, the PEP must make a local decision. The default matrix:

| Tool category | Failure mode | Rationale |
|---|---|---|
| Destructive (delete, overwrite, exec) | **Fail closed** — deny and emit local audit log | Safety over availability |
| Network-outbound | **Fail closed** — deny and emit local audit log | Exfiltration risk |
| File-write | **Fail closed** — deny and emit local audit log | Data integrity |
| File-read / search | **Fail open** — allow, emit local audit log | Low risk; availability matters |
| Read-only introspection | **Fail open** — allow, emit local audit log | No destructive surface |

**Configurable overrides:** commandments can specify `on_daemon_unreachable: fail_open | fail_closed` per rule. The above are defaults when no override is set.

**Timeouts and retry:**
- Default timeout for `gate.evaluate`: 100ms (latency budget for local Unix socket)
- No retry — single attempt only; timeout → local decision
- PEPs cache `allow` decisions for the same `(tool, targets)` fingerprint for up to 5s to reduce IPC chatter on repeated identical actions

**Proxy PEP vs local PEP defaults differ:** the proxy fails with an upstream error (existing behavior, no change). Local PEPs use the table above.

---

## 6. Execution Lifecycle and Correlation

Pre-exec gate decisions and post-hoc logwatcher observations must be linked to avoid two unrelated audit streams.

### Correlation ID

`gate.evaluate` returns a `gate_event_id` (UUID). This is the stable correlation handle for the entire tool execution lifecycle:

```
pre-exec decision:   gate_event_id = "evt-abc"
  ↓ agent executes tool
post-exec JSONL:     crabwise_gate_event_id = "evt-abc"  (if agent threads it through)
logwatcher event:    correlates via crabwise_gate_event_id field
```

### Propagation

For Claude Code hook-based PEPs: the `PreToolUse` hook output can include metadata that appears in the post-hoc JSONL. This is the preferred correlation path. The hook writes `gate_event_id` into the hook output metadata so the JSONL event carries it.

For shell-wrapping PEPs (`crab-shell`): correlation from pre-exec to post-hoc logwatcher is **best-effort only**. The agent controls JSONL format; we cannot inject correlation IDs into events the agent writes itself. The gate audit event and logwatcher event may not be linkable for externally-controlled JSONL.

### Deduplication

When both a gate event and a logwatcher event exist for the same action:
- If `gate_event_id` is present in the logwatcher event: deduplicate by ID, treat them as one lifecycle
- If not: treat them as independent observations; no deduplication

Blocked actions only generate a gate event (logwatcher never sees the execution because it never happened).

---

## 7. PEP Implementation Tiers

### Tier 1 — Semantic PEPs (implement first)

#### Network proxy PEP (exists)
HTTP MITM proxy intercepts all LLM API calls. Calls the commandment engine (`shouldBlock()`) synchronously before forwarding.

**Gap:** Only covers network traffic. Local tools invisible.

#### Tool invocation PEP (new — primary gap closure)

A generic hook that fires before ANY local tool execution, not just shell. The PEP:

1. Receives structured tool intent from the agent (name, category, structured targets).
2. Calls `gate.evaluate` via the Unix socket.
3. If `allow`: proceeds.
4. If `block`: returns error to agent (non-zero exit code + structured JSON for hook-based integrations).

**Implementations (adapters of this PEP):**

| Adapter | How it intercepts | Coverage |
|---|---|---|
| Claude Code `PreToolUse` hook | Hook fires before any CC tool | All CC tools (Bash, Write, Edit, Read, Glob, etc.) |
| `crab-shell` binary | Wraps `/bin/bash`; receives argv before exec | Shell commands only |
| File tool shim | Future — wraps file write/edit operations | File writes/deletes |

For Claude Code, the hook is the preferred primary integration because it fires at the agent's tool-intent boundary before any shell interpretation and covers all tool types with a single integration point.

`crab-shell` is an adapter for the shell tool category specifically, useful when hook-based integration is unavailable (e.g., running bare shell scripts via crabwise without a Claude Code hook).

#### In-process library PEP (for cooperative open-source agents)

For agents where crabwise has deep integration (e.g., OpenClaw), embed a lightweight PDP client as a library. Library makes a function call (no Unix socket round-trip) to evaluate actions. Internally routes to daemon or uses a shared-memory decision cache.

**When to use:** Performance-critical agents where even 100ms timeout budget is too high. Requires agent cooperation and SDK linking.

---

### Tier 2 — Platform-specific containment (safety net, not primary enforcement)

For uncooperative or closed-source agents with no hook mechanism: platform-specific containment backends apply coarse hard limits. These are **not a coherent portable PEP** — each platform requires a separate implementation:

| Platform | Mechanism | Capability |
|---|---|---|
| Linux | seccomp-BPF | Synchronous syscall filtering; no semantic visibility |
| Linux | eBPF (LSM hooks) | Asynchronous; can observe but not easily block synchronously |
| macOS | Endpoint Security | Process/file event callbacks; requires entitlement |
| macOS | Sandbox profiles | Static rules; not dynamically updatable |

Classic pitfalls apply to all: TOCTOU races on file paths, argument reconstruction imprecision, kernel version dependencies. Deploy beneath Tier 1 PEPs as a hard outer fence, not as the primary semantic enforcement layer.

For ambiguous actions, a sidecar can call `gate.evaluate` with a reconstructed (best-effort) semantic event, but the reconstruction quality is lower than hook-based integration.

---

### Tier 3 — Configuration provisioning (for closed platforms)

For SaaS-style agents with their own native policy controls, crabwise acts as the source-of-truth and pushes desired policy configuration into the agent's own settings. No inline PEP needed.

**Limitation:** Coverage bounded by whatever knobs the platform exposes.

---

## 8. What Changes vs. What Doesn't

### What changes

| Item | Change |
|---|---|
| IPC server | Add `gate.evaluate` JSON-RPC method with structured `targets` schema |
| New: Tool invocation PEP | Generic hook adapter + Claude Code `PreToolUse` integration as first impl |
| New binary: `crab-shell` | Shell-specific adapter of the tool invocation PEP |
| `CanEnforce()` on `Adapter` | Deprecate — adapters are observers/translators; PEPs are the enforcement layer |
| Docs | PEP/PDP model for adapter contributors |

### What doesn't change

| Item | Status |
|---|---|
| Commandment engine | Unchanged — `gate.evaluate` calls it |
| Audit event pipeline | Unchanged — blocked gate events emit via existing pipeline |
| Existing adapter interface | Unchanged for event emission |
| Proxy enforcement | Unchanged — continues to call `shouldBlock()` internally |
| All existing tests | Unchanged |

---

## 9. Audit Events for Gate Decisions

When `gate.evaluate` is called, the daemon emits an `AuditEvent` regardless of outcome:

| Field | Value |
|---|---|
| `ID` | = `gate_event_id` (correlation handle) |
| `ActionType` | tool category from request |
| `Outcome` | `blocked` or `allowed` |
| `AdapterType` | `gate` |
| `Arguments` | includes structured targets, commandment ID, reason |

Emitting for both allowed and blocked actions enables complete pre-exec audit coverage. For blocked actions, this is the only event (execution never happens). For allowed actions, this links to any subsequent post-hoc logwatcher event via `gate_event_id`.

---

## 10. Unresolved Questions

1. Should `gate.evaluate` always emit an audit event for `allow` decisions, or only on `block`? (Affects storage volume at high tool-call rates.)
2. Should `CanEnforce()` be removed from `Adapter` now or kept with a deprecation notice for any external consumers?
3. What is the exact format for Claude Code `PreToolUse` hook output to carry `gate_event_id` into the JSONL?

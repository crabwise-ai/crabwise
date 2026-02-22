# Crabwise AI — Prototype Architecture Plan (2026)

## Vision

Crabwise AI is the oversight layer between AI agents, their providers, and the people who use them. One daemon to monitor what your agents are doing, set rules they cannot break, and maintain a complete audit trail — across every agent, every provider.

This prototype targets the **open-source developer tool** — the first thing an engineer installs alongside their AI coding agents. One-line install, immediate visibility, zero configuration.

---

## Executive Assessment (2026)

The architecture direction is correct: **local-first, single-binary Go daemon, and progressive adapter adoption (log watcher → proxy)** are the right choices for developer trust and fast adoption.

The key delivery risk is not feature breadth; it is operational hardening in four areas:

1. **Streaming reliability** (SSE passthrough and cancellation behavior)
2. **Policy determinism** (safe, bounded, conflict-free matching)
3. **Audit integrity** (tamper evidence, redaction, forensic metadata)
4. **Install/runtime trust** (socket authz, permissions, binary integrity)

This plan now treats those as first-class scope with milestone gates, not polish.

---

## Core Mental Model

Crabwise sits between three things: the agent, the provider, and the user. The daemon wraps agent sessions with three layers of control:

```
┌──────────────────── Your Machine ──────────────────────┐
│                                                         │
│  ┌───────────┐  ┌───────────┐                          │
│  │Claude Code │  │  OpenClaw │                          │
│  └─────┬─────┘  └─────┬─────┘                          │
│        │               │                                │
│  ┌─────▼───────────────▼──────────────────────┐        │
│  │           Crabwise Daemon                   │        │
│  │                                             │        │
│  │  ┌─────────────────────────────────────┐   │        │
│  │  │  ADAPTER LAYER (observe + intercept)│   │        │
│  │  │                                     │   │        │
│  │  │  ┌─────────┐ ┌─────────┐ ┌───────┐│   │        │
│  │  │  │ Native  │ │Log Watch│ │ Proxy ││   │        │
│  │  │  │(MCP,    │ │(Claude  │ │(HTTP  ││   │        │
│  │  │  │ OpenClaw│ │ Code,   │ │ route)││   │        │
│  │  │  │         │ │ Cursor) │ │       ││   │        │
│  │  │  │ Full    │ │ Monitor │ │ Full  ││   │        │
│  │  │  │ control │ │ + audit │ │control││   │        │
│  │  │  └─────────┘ └─────────┘ └───────┘│   │        │
│  │  └──────────────┬──────────────────────┘   │        │
│  │                 │                           │        │
│  │  ┌──────────────▼──────────────────────┐   │        │
│  │  │  COMMANDMENT ENGINE (enforce)       │   │        │
│  │  │  - rule evaluation (<10ms)          │   │        │
│  │  │  - hard block / confirm / warn      │   │        │
│  │  │  - infrastructure-level, not prompt │   │        │
│  │  └──────────────┬──────────────────────┘   │        │
│  │                 │                           │        │
│  │  ┌──────────────▼──────────────────────┐   │        │
│  │  │  AUDIT LOGGER (record)              │   │        │
│  │  │  - OTel GenAI span emission         │   │        │
│  │  │  - structured event log (SQLite)    │   │        │
│  │  │  - who, what, when, where, outcome  │   │        │
│  │  └─────────────────────────────────────┘   │        │
│  │                                             │        │
│  └──────────────────┬──────────────────────────┘        │
│                     │                                    │
│  ┌──────────────────▼──────────────────────────┐        │
│  │  SQLite (durable local storage)              │        │
│  │  - audit events, commandments, agent state   │        │
│  └──────────────────────────────────────────────┘        │
│                                                          │
│  ┌──────────────────────────────────────────────┐        │
│  │  Crabwalk (CLI + TUI)                        │        │
│  │  - live agent monitor                        │        │
│  │  - query audit history                       │        │
│  │  - manage commandments                       │        │
│  └──────────────────────────────────────────────┘        │
│                                                          │
└──────────────────────────────────────────────────────────┘

        Optional (future):
        ┌──────────────────────┐
        │  Crabwise Cloud      │
        │  - team sync         │
        │  - shared policies   │
        │  - push notifications│
        └──────────────────────┘
```

---

## Top User Stories

1. **As a dev**, I want to install Crabwise in one command and immediately see what Claude Code and OpenClaw are doing on my machine — without changing any agent config.
2. **As a dev**, I want YAML rules like "never run rm -rf" enforced at the infrastructure layer so agents can't bypass them via prompt injection.
3. **As a dev**, I want a searchable, exportable audit trail of every agent action — tool calls, file access, commands, AI requests — with cost tracking.
4. **As a security-conscious dev**, I want warnings when agents access sensitive files (`.env`, `.key`, `.pem`, `.ssh/`) and the ability to block destructive commands.

---

## Scope

### In Scope

1. **Crabwise Daemon** — background process monitoring AI agent activity on the host
2. **Adapter layer** with two initial adapters:
   - **Log Watcher** — parse Claude Code session logs (monitor + audit, read-only)
   - **Proxy** — local HTTP proxy any agent can route through (monitor + enforce)
3. **Commandment Engine** — YAML-defined rules, infrastructure-level enforcement
   - Two levels for prototype: **hard block** and **warn**
4. **Audit Logger** — structured event log in local SQLite, OTel-compatible span emission
5. **Crabwalk CLI/TUI** — terminal interface for live monitoring, audit queries, commandment management
6. **Agent discovery** — detect running agents via process scanning + log file detection
7. **Local-first storage** — SQLite for everything, works fully offline

### Out of Scope (prototype)

- Dashboard UI (CLI/TUI only)
- Crabbot (AI agent interface)
- Cloud sync / team features
- Notifications (Slack, Discord, etc.)
- Native MCP adapter (future tier)
- Visual activity graph / timeline replay
- Windows / macOS support (Linux first)
- Anomaly detection
- Confirm enforcement level (requires interaction pattern — v2)
- Central server / multi-host aggregation

---

## Prototype SLOs and Acceptance Gates

These are release gates for each milestone, not aspirational goals.

| Area | Gate |
|------|------|
| Commandment evaluation latency | `p95 < 2ms`, `p99 < 8ms` |
| Proxy overhead (non-stream) | Added `p95 < 20ms` |
| Proxy streaming overhead | First-token delta `< 50ms` |
| Reliability | `0` event loss under nominal load; overload loss explicitly metered and surfaced |
| Daemon footprint | RSS `< 80MB` under dual-adapter active load |
| Security | Redaction tests pass; blocked actions never reach upstream provider |

---

## Tech Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| **Daemon** | Go | Single binary, low footprint, /proc access, cross-compiles. Every major host agent (Datadog, Grafana Agent, Telegraf) uses Go for these reasons. |
| **Local storage** | SQLite (`modernc.org/sqlite` in prototype) | Local-first. Zero ops, works offline, durable. Pure-Go simplifies Linux cross-compile; storage layer remains swappable to `mattn/go-sqlite3` if perf requires. |
| **Telemetry** | OpenTelemetry SDK | Industry standard. GenAI semantic conventions. Compatible with whatever observability stack the user runs. |
| **CLI** | Go (Cobra + Bubble Tea) | Ships in same binary as daemon. Cobra for commands, Bubble Tea for live TUI. |
| **Config** | YAML | Human-readable, git-friendly, versionable. |

### Why Not TypeScript / Node?

The daemon runs alongside agents on developer machines. Needs to be invisible — low memory, low CPU, fast startup, single binary. Node's runtime overhead (~30-50MB baseline) and weaker syscall integration make it wrong for a background daemon. Go gives a <10MB binary that starts instantly and uses <20MB RAM.

Future Crabwalk dashboard and Crabbot will likely use TypeScript. The daemon should not.

---

## Adapter Architecture

Each adapter implements a common interface but uses a different observation method.

```go
type Adapter interface {
    ID() string
    Type() AdapterType  // Native, LogWatcher, Proxy
    Detect() bool       // Can this adapter find its target on this machine?
    Start(ctx context.Context, events chan<- AgentEvent) error
    Stop() error
    CanEnforce() bool   // LogWatcher = false, Proxy/Native = true
}
```

### Adapter Tiers

| Tier | Method | Prototype Targets | Enforcement |
|------|--------|-------------------|-------------|
| **Log Watcher** | Parse agent log files + process activity | Claude Code | Monitor + audit only |
| **Proxy** | Local HTTP proxy agents route through | Any HTTP-based agent (OpenClaw, Ollama, etc.) | Full (monitor + enforce) |
| **Native** | Direct API/protocol integration | (future: MCP, OpenClaw gateway) | Full |

### Log Watcher Adapter (Claude Code)

Claude Code writes structured JSONL session logs. The adapter:

1. Detects Claude Code installation (process, `~/.claude/` directory)
2. Auto-discovers all projects under `~/.claude/projects/` (configurable include/exclude)
3. Tails JSONL session files in real time
4. Extracts: tool calls, file access, commands executed, model, token counts
5. Emits normalized `AuditEvent` structs

Read-only visibility — can see and record everything but can't intercept. For a developer's first experience ("install Crabwise, immediately see what Claude Code is doing"), this is the highest-value, lowest-friction adapter.

### Proxy Adapter (OpenAI-compatible)

Transparent HTTP proxy on `localhost:9119`:

```bash
export OPENAI_BASE_URL=http://localhost:9119/v1
```

The proxy:

1. Receives request
2. Evaluates Commandments before forwarding
3. Forwards to real provider (or blocks)
4. Captures response metadata (tokens, model, finish reason, cost)
5. Streams response back to caller
6. Emits `AuditEvent` with full GenAI telemetry

Prototype adapter: **OpenAI-compatible** (covers OpenAI, Azure OpenAI, OpenClaw, and anything using the same API shape). Anthropic adapter in fast follow.

**SSE streaming must work correctly.** Non-negotiable. If the proxy breaks streaming or adds perceptible latency, developers will uninstall immediately.

### Event Pipeline Reliability Contract

1. Adapters emit into a **bounded in-memory queue** (configurable, default e.g. 10k events)
2. Queue overload behavior is explicit and observable:
   - preferred: block producer briefly with timeout
   - fallback: drop oldest non-critical events
3. Every dropped event increments counters and emits a synthetic `pipeline_overflow` audit event
4. `crabwise status` and `crabwise watch` expose queue depth + dropped event counters
5. Background SQLite writer uses batched commits for steady-state throughput

---

## Commandment Engine

Commandments are infrastructure-level rules that override all agent behavior. Enforced on the wire, not in the prompt — cannot be jailbroken.

### Schema

```yaml
version: "1"
commandments:

  - name: no-destructive-commands
    description: "Block destructive shell commands"
    match:
      action_type: command_execution
      content:
        pattern: "rm -rf|DROP TABLE|format |mkfs"
    enforcement: block
    message: "Blocked: destructive command detected"

  - name: protect-credentials
    description: "Warn when agents access sensitive files"
    match:
      action_type: file_access
      path:
        pattern: "*.env|*.key|*.pem|*credentials*|.ssh/*|.aws/*"
    enforcement: warn
    message: "Agent accessed a sensitive file"

  - name: daily-spend-limit
    description: "Block when daily spend exceeds threshold"
    match:
      action_type: ai_request
      daily_cost_usd:
        gt: 50
    enforcement: block
    message: "Daily spend limit ($50) reached"

  - name: allowed-models
    description: "Only allow approved models via proxy"
    match:
      action_type: ai_request
      model:
        not_in: [gpt-4o, gpt-4o-mini, claude-sonnet-4-5-20250929]
    enforcement: block
    message: "Model not in approved list"

  - name: no-push-to-main
    description: "Warn when agents push to main"
    match:
      action_type: command_execution
      content:
        pattern: "git push.*main|git push.*master"
    enforcement: warn
    message: "Agent attempted to push to main branch"
```

### Enforcement Levels (Prototype)

| Level | Behavior | Adapter Requirement |
|-------|----------|-------------------|
| **Block** | Action intercepted and stopped. Agent receives error. | Proxy or Native only |
| **Warn** | Action proceeds, flagged in audit log. | Any adapter |

**Confirm** (pause + prompt user) deferred to v2.

### Evaluation Path

```
Event arrives from adapter
  → Match against all active commandments
  → If match:
      → block (proxy/native): return error to agent
      → block (log watcher): downgrade to warn (enforced: false)
      → warn: log with commandment_triggered flag, continue
  → If no match: pass through
  → Log evaluation result regardless

Hot-path requirements:
- deterministic rule order (`priority`, then declaration order)
- precompiled regex/glob matchers
- bounded matcher execution (no unbounded regex backtracking)
- `p95 < 2ms`, `p99 < 8ms` per evaluation
```

---

## Audit Logger

### Event Schema

```go
type AuditEvent struct {
    ID            string
    Timestamp     time.Time

    // Who
    AgentID       string    // "claude-code", "openclaw", custom
    AgentPID      int

    // What
    ActionType    string    // ai_request, command_execution, file_access, tool_call
    Action        string    // human-readable description
    Arguments     string    // payload

    // Where
    SessionID     string
    WorkingDir    string
    ParserVersion string

    // Outcome
    Outcome       string    // success, failure, blocked, warned

    // Commandment evaluation
    CommandmentsEvaluated []string
    CommandmentsTriggered []string

    // AI-specific
    Provider      string
    Model         string
    InputTokens   int
    OutputTokens  int
    CostUSD       float64

    // Detection
    AdapterID     string
    AdapterType   string    // log_watcher, proxy, native

    // Integrity + troubleshooting
    RawPayloadRef string    // optional pointer to sidecar compressed raw payload blob (not inlined in events table)
    PrevHash      string    // hash-chain predecessor
    EventHash     string    // hash(current event + PrevHash)
    Redacted      bool
}
```

### OTel Span Attributes (GenAI Semantic Conventions)

```
Span: gen_ai.request
├── gen_ai.system = "openai"
├── gen_ai.request.model = "gpt-4o"
├── gen_ai.usage.input_tokens = 1200
├── gen_ai.usage.output_tokens = 350
├── gen_ai.response.finish_reason = "stop"
├── crabwise.agent_id = "claude-code"
├── crabwise.session_id = "abc123"
├── crabwise.adapter_type = "proxy"
├── crabwise.commandments.evaluated = ["allowed-models", "daily-spend-limit"]
├── crabwise.commandments.triggered = []
├── crabwise.cost_usd = 0.0043
└── crabwise.outcome = "success"
```

### Storage

SQLite with indexes on `timestamp`, `agent_id`, `action_type`, `outcome`. Default retention: 30 days. Future: CSV/JSON export, cloud sync.

Raw payload blobs are stored as sidecar compressed files (e.g., `~/.local/share/crabwise/raw/<event-id>.zst`); SQLite stores only `RawPayloadRef`. Blob retention follows audit retention and is garbage-collected on the same schedule.

Additional prototype requirements:
- file permissions `0600` for DB and local config files
- tamper-evident hash-chain validation command (`crabwise audit --verify-integrity`)
- **Audit redaction (mandatory):** redact sensitive data before persistence/emission (SQLite, OTel spans, CLI/TUI, exports) across all adapters, including log-watcher-derived events
- **Proxy egress redaction (mandatory):** redact known credential patterns in outbound proxy payloads before forwarding upstream (with explicit allowlist controls)
- Crabwise never mutates original third-party log files (e.g., Claude Code logs); redaction applies only to Crabwise-managed data paths

---

## Crabwalk CLI

Ships in same binary as daemon. Developer's primary interface.

```bash
# Daemon
crabwise start                      # Start daemon in background
crabwise stop                       # Stop daemon
crabwise status                     # Health + discovered agents

# Live monitoring (Crabwalk TUI)
crabwise watch                      # Real-time agent activity feed
crabwise watch --agent claude-code  # Filter to one agent

# Audit
crabwise audit                      # Last hour
crabwise audit --since 24h          # Time range
crabwise audit --agent claude-code  # By agent
crabwise audit --action ai_request  # By type
crabwise audit --triggered          # Only commandment-triggered events
crabwise audit --cost               # Cost summary by agent/day
crabwise audit --export json        # Export

# Commandments
crabwise commandments list          # Active rules
crabwise commandments add <file>    # Add from YAML
crabwise commandments test <event>  # Dry-run test

# Agents
crabwise agents                     # List discovered agents
crabwise agents <id>                # Agent detail
```

### Crabwalk TUI (Bubble Tea)

```
┌─ Crabwalk ─────────────────────────────────────────────────┐
│ Agents: 2 active │ Events: 847 today │ Cost: $4.23 today   │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│ 14:23:05 [claude-code]  ai_request  gpt-4o  1.2k tok $0.02 │
│ 14:23:04 [claude-code]  tool_call   read_file src/main.ts   │
│ 14:23:01 [openclaw]     ai_request  gpt-4o-mini  340 tok    │
│ 14:22:58 [claude-code]  command     git diff --staged        │
│ 14:22:55 [openclaw]     tool_call   send_message             │
│ 14:22:50 [claude-code]  ai_request  gpt-4o  2.1k tok $0.04  │
│ 14:22:45 [claude-code]  command     npm test           ← WARN│
│                                                             │
├─────────────────────────────────────────────────────────────┤
│ ⚠ 2 warnings today │ 0 blocked │ q:quit  f:filter  /:search│
└─────────────────────────────────────────────────────────────┘
```

---

## Agent Discovery

Two methods, running continuously:

### 1. Process Scanning
Poll `/proc` for known agent signatures:
- `claude` / `claude-code` process
- OpenClaw processes + `OPENCLAW_*` env vars
- Any process with `CRABWISE_AGENT_ID` env var set

### 2. Log File Detection
Check known log locations:
- Claude Code: `~/.claude/projects/` JSONL session files
- OpenClaw: gateway logs, session data

Auto-discovered agents immediately start being monitored via appropriate adapter. No registration required.

Custom agents via config:
```yaml
# ~/.config/crabwise/config.yaml
agents:
  - id: my-custom-agent
    adapter: proxy
    process_match: "python my_agent.py"
```

---

## Project Structure

```
crabwise/
├── cmd/
│   └── crabwise/
│       └── main.go                 # Single binary entry (daemon + CLI)
│
├── internal/
│   ├── daemon/
│   │   ├── daemon.go               # Lifecycle (start, run, stop)
│   │   └── config.go               # Config loading
│   │
│   ├── adapter/
│   │   ├── adapter.go              # Adapter interface
│   │   ├── logwatcher/
│   │   │   ├── logwatcher.go       # Base log watcher
│   │   │   └── claude_code.go      # Claude Code JSONL parser
│   │   └── proxy/
│   │       ├── proxy.go            # HTTP proxy server
│   │       ├── streaming.go        # SSE passthrough
│   │       └── openai.go           # OpenAI-compatible adapter
│   │
│   ├── commandments/
│   │   ├── engine.go               # Rule evaluator
│   │   ├── parser.go               # YAML rule parser
│   │   └── matchers.go             # Pattern matching
│   │
│   ├── audit/
│   │   ├── logger.go               # Event logging to SQLite
│   │   ├── events.go               # Event type definitions
│   │   ├── query.go                # Query builder
│   │   └── otel.go                 # OTel span emission
│   │
│   ├── discovery/
│   │   ├── scanner.go              # Process + log file scanning
│   │   └── registry.go             # In-memory agent registry
│   │
│   ├── cli/
│   │   ├── root.go                 # Cobra root command
│   │   ├── start.go
│   │   ├── watch.go                # Bubble Tea TUI
│   │   ├── audit.go
│   │   ├── commandments.go
│   │   └── agents.go
│   │
│   └── store/
│       ├── sqlite.go               # Connection + migrations
│       └── schema.sql
│
├── configs/
│   ├── default.yaml                # Default daemon config
│   └── commandments.example.yaml   # Example commandments
│
├── scripts/
│   └── install.sh
│
├── docs/
│   ├── prd.md                      # Existing
│   └── THEPRODUCT_AI_Agent_Watchdog_Architecture.md  # Existing
│
├── plan/                            # This plan
│
├── Dockerfile
├── go.mod
├── LICENSE                          # MIT
└── README.md
```

---

## Daemon Lifecycle

```
STARTUP:
1. Load config from ~/.config/crabwise/config.yaml
2. Open/create SQLite at ~/.local/share/crabwise/crabwise.db
3. Run schema migrations
4. Load commandments from config and precompile all matchers
5. Create bounded event pipeline queue + metrics counters
6. Initialize Unix socket with restrictive permissions
7. Start agent discovery (process scan + log detect)
8. Start adapters for discovered agents
   ├─ Log watchers begin tailing
   └─ Proxy listener starts on :9119 (if enabled)
9. Start OTel export loop (optional)
10. Ready

STEADY STATE:
- Log watchers tail and parse continuously
- Proxy handles requests as they arrive
- All events flow through queue → commandment engine → audit logger
- Backpressure policy enforced if queue saturation occurs
- All evaluation outcomes logged (including downgrade from block→warn on non-enforcing adapters)

Every 10s:
- Rescan for new/exited agent processes
- Start/stop adapters as agents appear/disappear

Every 30s:
- Batch-export OTel spans (if collector configured)

On SIGHUP:
- Reload config + commandments atomically
- Keep old rules active if reload/compile fails

CLI ↔ Daemon:
- Unix socket for IPC
- CLI queries audit log, agent state, commandments
```

---

## Install Experience

```bash
# Install
curl -fsSL https://crabwise.dev/install.sh | sh

# Start
crabwise start

# Watch
crabwise watch
```

Downloads single Go binary (platform-detected), places in `~/.local/bin` or `/usr/local/bin`, creates default config, optionally starts as user-level systemd service.

Installer hardening requirements:
- publish SHA256 checksums and signature for binaries
- installer verifies checksum/signature before activation

**No Docker. No database. No cloud account.**

---

## Milestone Plan

### M0 — Skeleton + Agent Discovery (Week 1)
- [ ] Go project scaffold with Cobra CLI
- [ ] Daemon boots, reads config, opens SQLite, runs migrations
- [ ] Process scanner: detect Claude Code / OpenClaw via `/proc`
- [ ] Agent registry: in-memory map of discovered agents
- [ ] `crabwise start` / `stop` / `status`
- [ ] `crabwise agents` — list discovered agents
- [ ] Unix socket for CLI ↔ daemon IPC

**M0 Exit Gates**
- [ ] Daemon can start/stop cleanly for 100 cycles without orphaned processes
- [ ] IPC socket enforces local-user-only permissions

### M0.5 — Hardening Baseline (Week 1.5)
- [ ] Threat model for local attacker + malicious prompt-induced actions
- [ ] Secret redaction pipeline with explicit scopes: audit persistence/emission + proxy egress forwarding paths
- [ ] Bounded event queue + backpressure/drop accounting
- [ ] Benchmark harness for eval latency, proxy overhead, memory footprint
- [ ] Config reload mechanism (`SIGHUP`) with atomic rule swap

**M0.5 Exit Gates**
- [ ] Queue overflow behavior is deterministic and observable
- [ ] Redaction tests pass for `.env`, keys, tokens, and common credential formats on both audit path and proxy egress path; source third-party logs remain untouched
- [ ] Baseline benchmark report checked in

### M1 — Log Watcher + Audit Trail (Week 2)
- [ ] Claude Code log watcher: locate JSONL sessions, tail, parse
- [ ] Extract events: tool calls, file access, commands, AI requests
- [ ] Normalize into `AuditEvent` structs
- [ ] SQLite audit logger: write events, indexed queries
- [ ] `crabwise audit` with filters (agent, action, time)
- [ ] `crabwise audit --export json`
- [ ] Token counting + cost estimation
- [ ] Schema-versioned parser + `raw_payload` fallback capture
- [ ] Hash-chain event integrity fields persisted

**M1 Exit Gates**
- [ ] `0` parse panics on malformed/unknown log records
- [ ] Contract fixtures from real Claude logs pass in CI
- [ ] `crabwise audit --verify-integrity` validates full chain

### M2 — Commandment Engine (Week 3)
- [ ] YAML commandment parser
- [ ] Pattern matching: regex, glob, numeric comparisons, list membership
- [ ] Evaluation loop: every event checked against active commandments
- [ ] `warn` enforcement (flag in audit, surface in CLI)
- [ ] `crabwise commandments list` / `add` / `test`
- [ ] Starter commandment pack
- [ ] Commandment metadata on every audit event
- [ ] Deterministic rule priority + conflict resolution semantics
- [ ] Precompiled matcher cache with bounded evaluation

**M2 Exit Gates**
- [ ] Eval latency meets SLO (`p95 < 2ms`, `p99 < 8ms`)
- [ ] Rule ordering and conflict behavior covered by tests

### M3 — Proxy + Active Enforcement (Week 4)
- [ ] HTTP proxy on `localhost:9119`
- [ ] OpenAI-compatible transparent passthrough
- [ ] SSE streaming (bulletproof)
- [ ] Commandment evaluation in request hot path (SLO-bound)
- [ ] `block` enforcement: return error before request reaches provider
- [ ] Full GenAI telemetry: model, tokens, cost, finish reason
- [ ] `crabwise audit --cost`
- [ ] Streaming torture suite (chunking, flush, disconnect, cancel, timeout)

**M3 Exit Gates**
- [ ] Proxy added latency meets SLO (`p95 < 20ms`)
- [ ] Streaming first-token delta `< 50ms`
- [ ] Blocked requests are never forwarded upstream (asserted in tests)

### M4 — Crabwalk TUI + Polish (Week 5)
- [ ] Bubble Tea live TUI: real-time feed, agent status, cost counter
- [ ] Filter by agent, action type, commandment triggers
- [ ] Warning/block indicators
- [ ] OTel span emission for all events
- [ ] Install script (curl one-liner)
- [ ] Cross-compile: Linux amd64 + arm64
- [ ] `commandments.example.yaml` with sensible defaults
- [ ] README with setup + usage
- [ ] MIT license

**M4 Exit Gates**
- [ ] TUI shows queue depth, drop counters, and commandment trigger rate
- [ ] Binary verification path documented in installer output
- [ ] Footprint SLO met under representative dual-adapter workload

---

## Verification Plan

1. **Agent discovery:** Start Claude Code + OpenClaw → `crabwise agents` shows both discovered
2. **Log watcher audit:** Use Claude Code normally → `crabwise audit` shows tool calls, commands, AI requests with token counts
3. **Parser drift safety:** Replay mixed-version JSONL fixtures → unknown records captured via `raw_payload` without crashes
4. **Commandment warn:** Add `protect-credentials` commandment → Claude Code reads `.env` → `crabwise audit --triggered` shows warning
5. **Proxy enforcement:** Set `OPENAI_BASE_URL=localhost:9119` → send request for blocked model → denied, logged in audit
6. **Proxy streaming torture:** Verify SSE correctness under chunking, disconnects, cancellation, and timeout conditions
7. **Cost tracking:** `crabwise audit --cost` shows spend by agent and day
8. **Audit integrity:** `crabwise audit --verify-integrity` validates event chain
9. **TUI + reliability:** `crabwise watch` shows live feed, warnings/blocks, queue depth, and dropped-event counters
10. **Redaction scope:** verify sensitive payloads are redacted in Crabwise DB/OTel/CLI exports and in proxy-forwarded upstream payloads; confirm original Claude logs are unchanged
11. **Performance gates:** benchmark suite confirms latency/footprint SLOs before milestone sign-off

---

## Key Design Decisions

**Why local-first with SQLite?** AI agents access codebases, credentials, files. Telemetry data is sensitive by default. Local-first means data never leaves the machine unless opted in. SQLite is zero ops, single file, works offline. Trust differentiator vs cloud-first competitors.

**Why Log Watcher before Proxy?** Delivers immediate value with zero setup. Install Crabwise, start daemon, instantly see what Claude Code is doing — no env var changes. Proxy is opt-in upgrade for active enforcement. Minimizes adoption friction.

**Why single Go binary?** Devs have strong opinions about what runs on their machines. Single binary, no runtime deps, no Docker = lowest friction install. CLI and daemon ship together.

**Why `modernc.org/sqlite` first?** Prototype needs reproducible Linux cross-compiles without CGO toolchain friction. Keep storage behind interface and switch driver only if measured performance requires it.

**Why Commandments, not "policies"?** Naming matters. "Policies" sounds enterprise. "Commandments" communicates exactly what they are: immutable rules that cannot be overridden or jailbroken. Infrastructure-level, not suggestions.

---

## Key Reference Files

- `/home/del/Github/crabwise-genai/src/integrations/otel/` — OTel instrumentation patterns
- `/home/del/Github/crabwalk/src/integrations/openclaw/client.ts` — OpenClaw protocol reference
- `/home/del/Github/crabwalk/src/integrations/openclaw/parser.ts` — OpenClaw event parsing
- `/home/del/Github/crabwalk/documents/openclaw-events-*.json` — Real event data samples

## Decisions Logged (Resolved for Prototype)

1. **Claude Code log format handling**
   - Build schema-versioned parser with tolerant decoding and `raw_payload` fallback.
   - Ship fixture-based parser contract tests before M1 exit.

2. **OpenClaw adapter strategy**
   - Use **Proxy-first** for enforcement parity and uniform telemetry.
   - Keep optional log watcher as fallback for low-friction monitoring.

3. **SQLite driver choice**
   - Start prototype with **`modernc.org/sqlite`** for simpler cross-compiles.
   - Keep storage interface swappable; benchmark-driven switch to `mattn/go-sqlite3` only if needed.

4. **OTel deployment model**
   - OTel collector is **optional** in prototype.
   - Default remains local-only storage; direct OTLP export enabled only when configured.

## Open Risks to Revisit Post-Prototype

- Confirm-enforcement UX (`confirm` mode) interaction design in v2.
- Windows/macOS process discovery parity and file permission semantics.
- Native MCP adapter roadmap sequencing against customer demand.

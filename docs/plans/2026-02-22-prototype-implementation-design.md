# Crabwise AI — Prototype Implementation Design

## Context

Revision of the prototype architecture plan. Restructures milestones into vertical slices (each delivers a demo-able user story), adds concrete implementation specs, and addresses gaps/risks in the original plan.

**Canonical architecture:** `plan/crabwise-prototype-architecture-plan.md`
**This document:** Implementation-ready design with specs, risk mitigations, and testing strategy.

---

## What We're Building

Single Go binary (`crabwise`) — local-first daemon + CLI/TUI that monitors AI agent activity, enforces YAML-defined commandments at infrastructure level, and maintains a tamper-evident audit trail. Targets Claude Code + Codex CLI (log watcher) and OpenAI-compatible agents (proxy).

## Top User Stories

1. **As a dev**, I install Crabwise in one command and immediately see what Claude Code is doing — without changing any agent config.
2. **As a dev**, I define YAML rules like "never run rm -rf" enforced at infrastructure layer so agents can't bypass them via prompt injection.
3. **As a dev**, I get a searchable, exportable audit trail of every agent action — tool calls, file access, commands, AI requests — with cost tracking.
4. **As a security-conscious dev**, I get warnings when agents access sensitive files and can block destructive commands.

## Architecture Diagrams

- [Technical architecture diagram](./2026-02-22-prototype-technical-architecture-diagram.md)
- [User flow diagram](./2026-02-22-prototype-user-flow-diagram.md)
- [Control plane governance diagram](./2026-02-22-prototype-control-plane-governance-diagram.md)

---

## Milestones (Vertical Slices)

### M0 — Foundation + First Value (Weeks 1-1.5) ✅ COMPLETE

**Demo:** `crabwise start && crabwise audit` shows Claude Code events

| Deliverable | Detail | Status |
|------------|--------|--------|
| Go scaffold | Cobra CLI, go.mod (`github.com/crabwise-ai/crabwise`), project structure | ✅ |
| Daemon lifecycle | start/stop/status, PID file, signal handling (SIGTERM/SIGINT) | ✅ |
| Config loading | `~/.config/crabwise/config.yaml` with sensible defaults | ✅ |
| SQLite | `~/.local/share/crabwise/crabwise.db`, WAL mode, schema migrations | ✅ |
| Agent discovery | /proc scanning for claude/codex processes, `~/.claude/` + `~/.codex/` log detection | ✅ |
| Log watcher adapters | Locate JSONL sessions via fsnotify, tail, parse into AuditEvents (Claude Code + Codex CLI) | ✅ |
| Audit writer | Batched SQLite inserts, hash-chain integrity | ✅ |
| CLI queries | `crabwise agents`, `crabwise audit` (time/agent/action filters), `--export json`, `--verify-integrity` | ✅ |
| IPC | Unix socket (JSON-RPC 2.0), local-user-only permissions | ✅ |
| Basic watch | `crabwise watch` — streaming text output (not TUI), live event tail | ✅ |
| **CI/CD** | **GitHub Actions (lint/test/build), GoReleaser, Dependabot** | **✅ (unplanned)** |
| **Install script** | **`curl \| bash` installer with OS/arch detection, version pinning** | **✅ (unplanned, pulled from M3)** |
| **Origin tracing** | **`hostname` + `user_id` (kernel UID) on every audit event** | **✅ (unplanned)** |
| **Platform support** | **Build constraints for darwin (macOS) IPC, GoReleaser cross-compile** | **✅ (unplanned)** |

**Pre-gate:** CC log fixtures captured + anonymization script run before parser development begins.

**Exit gates:**
- Daemon starts/stops cleanly for 100 cycles, no orphans
- CC log events parsed and queryable in audit
- Hash chain validates end-to-end
- Zero parse panics on malformed/unknown records (captured via raw_payload)
- IPC socket permissions enforced (0700 dir, 0600 socket, SO_PEERCRED verified)
- `crabwise watch` streams events in real time

**Post-M0 changes (not in original plan):**
- CC parser: added `progress` record type (skip), fixed `toolUseResult` as `json.RawMessage` (CC sends objects, not strings)
- Audit events: added `hostname` (os.Hostname) and `user_id` (os.Getuid, kernel-verified) fields for origin tracing — included in hash chain, persisted via migration 002
- CI/CD: GitHub Actions with golangci-lint v2, race-detector tests, GoReleaser (linux+darwin, amd64+arm64), Dependabot for Go modules + Actions
- Install script: `curl | bash` with OS/arch detection, version pinning, sudo fallback (pulled forward from M3 de-scope list)
- Linting: `.golangci.yml` with errcheck exclusions for safe patterns (defer Close, fmt.Fprint, os.Remove), test file exclusions
- Platform: build constraints for SO_PEERCRED (Linux-only) vs no-op (darwin), enabling macOS builds

### M1 — Commandment Engine + Warn (Weeks 2-2.5)

**Demo:** `.env` access triggers warning in `crabwise audit --triggered`

| Deliverable | Detail |
|------------|--------|
| YAML parser | Commandment schema v1, validation |
| Pattern matching | Regex, glob, numeric comparisons, list membership |
| Precompiled matchers | Bounded execution, no catastrophic backtracking |
| Evaluation loop | Every event checked against active commandments |
| Warn enforcement | Flag in audit, surface in CLI output and watch |
| Commandment CLI | `list`, `test <event-json>`, `reload` |
| Starter pack | 4 default commandments (destructive cmds, credentials, approved models, git push main) |
| Audit metadata | `commandments_evaluated` + `commandments_triggered` on every event |
| Priority/conflicts | Deterministic rule order, conflict resolution |
| SIGHUP reload | Atomic rule swap, keep old rules on parse/compile failure |
| Audit redaction | Redact secrets/credentials in audit persistence path |

**Exit gates:**
- Eval latency: p95 < 2ms, p99 < 8ms
- Rule ordering + conflict resolution covered by tests
- Redaction tests pass for `.env`, API keys, tokens, common credential patterns
- SIGHUP reload works atomically
- `block` rules downgrade to `warn` on log watcher (non-enforcing adapter)

**Post-M1 changes (out of scope):**
- Added Codex CLI log watcher support (`internal/adapter/logwatcher/codexcli.go`) with parser routing by source/type.
- Added Codex discovery defaults (`codex` process signature, `~/.codex/sessions/` log path).
- Added Codex fixtures and end-to-end coverage (`testdata/codex-cli/`, parser tests, CLI audit integration test).
- Normalized Codex session IDs to UUID suffix across parser + discovery for consistent correlation in `agents`/`audit`.
- Fixed Codex token_count merge behavior to support partial `usage` payloads without dropping top-level token counts.

### M1.5 — Canonical Tool Taxonomy + Central Classifier (Week 3)

**Demo:** One commandment using `tool_category: shell` + `tool_effect: execute` matches equivalent tool actions across Claude Code and Codex without adapter-specific rules.

**Rationale:** M2 block enforcement needs provider-agnostic commandment semantics. Central classification removes per-adapter drift and keeps taxonomy decisions deterministic.

| Deliverable | Detail |
|------------|--------|
| Central classifier | Provider-aware classifier shared by all adapters and proxy paths |
| Deterministic exact lookup | Resolve in fixed order: provider exact → provider lowercase → `_default` lowercase |
| Heuristic ordering | Heuristic rules are evaluated top-to-bottom, first-match-wins |
| Overlap handling | Heuristic overlap is surfaced as a warning only (non-blocking load path) |
| Observability | Persist `classification_source`; surface `unclassified_tool_count` in `status`; feed fallback/unknowns into classifier drift review |
| CLI introspection | `crabwise classify <tool-name> --provider <p> --args key1,key2` for dry-run resolution and provenance debugging |
| DB stance | Fresh DB bootstrap for M1.5 with taxonomy fields in baseline schema (no migration narrative for this milestone section) |

**Exit gates:**
- Conformance fixtures show equivalent tool actions classify identically across adapters
- Known mapped tools resolve via deterministic exact matching (no silent heuristic downgrade)
- `crabwise status` exposes `unclassified_tool_count` and operators can use it to spot taxonomy drift
- `crabwise classify` output is deterministic and includes classification provenance

### M2 — Proxy + Block Enforcement (Weeks 3-4)

**Demo:** Disallowed model request denied before reaching provider; `crabwise audit` shows blocked event

| Deliverable | Detail |
|------------|--------|
| HTTP proxy | `localhost:9119`, OpenAI-compatible transparent passthrough |
| SSE streaming | Correct under chunking, flush, disconnect, cancel, timeout |
| Block enforcement | Commandment eval in request hot path, error returned to agent |
| GenAI telemetry | Model, tokens, cost, finish reason, provider |
| Cost tracking | Configurable per-model pricing, `crabwise audit --cost` |
| Proxy egress redaction | Redact credential patterns in outbound payloads before upstream |
| Event queue hardening | Bounded queue, backpressure/drop accounting, `pipeline_overflow` events |
| Raw payloads | Sidecar `.zst` blobs, logical ID ref (no path injection), hard limits enforced |

**Exit gates:**
- Proxy added latency: p95 < 20ms
- Streaming first-token delta: < 50ms
- Blocked requests never forwarded upstream (asserted in tests)
- Streaming torture suite passes
- Queue overflow deterministic and observable
- Egress redaction tests pass; original third-party logs untouched

### M3 — TUI + Polish + Release (Week 5)

**Demo:** `crabwise watch` shows live Bubble Tea TUI with agent status, cost counter, warnings

| Deliverable | Detail |
|------------|--------|
| Bubble Tea TUI | Real-time event feed, agent status, cost counter |
| TUI features | Filter by agent/action/triggers, warning/block indicators, queue depth + drop counters |
| OTel export | GenAI span emission, optional collector, local-first default |
| Install script | `curl -fsSL ... \| sh`, platform detection, checksum verification |
| Cross-compile | Linux amd64 + arm64 |
| Release artifacts | `commandments_default.yaml`, README, MIT license |
| Benchmark suite | All SLOs verified under dual-adapter load |

**Exit gates:**
- TUI shows queue depth, drop counters, commandment trigger rate
- RSS < 80MB under dual-adapter active load
- Binary verification documented in installer
- All SLOs confirmed by benchmark suite
- Install script works on fresh Ubuntu + Arch

---

## SLOs (Release Gates)

| Area | Gate |
|------|------|
| Commandment eval | p95 < 2ms, p99 < 8ms |
| Proxy overhead | p95 < 20ms |
| Streaming first-token | delta < 50ms |
| Event loss | 0 under nominal load; overflow metered |
| Daemon footprint | RSS < 80MB |
| Security | Redaction passes on both surfaces; blocked actions never reach upstream |

### SLO Measurement Profile

Benchmarks use this standard profile for reproducibility:

- **Request payload:** 4KB request body, 16KB response body (representative of chat completions)
- **Concurrency:** 10 concurrent clients for proxy tests
- **Commandment set:** 20 rules (mix of regex, glob, numeric, list membership)
- **Event volume:** 100 events/sec sustained for queue/audit tests
- **Hardware baseline:** 4-core, 8GB RAM Linux (CI runner or equivalent). Report actual hardware in benchmark output.
- **Measurement:** 10s warmup, 60s measurement window, report p50/p95/p99/max

---

## Implementation Specs

### Log Parsing Strategy (Claude Code + Codex CLI)

Claude Code logs at `~/.claude/projects/<project-hash>/sessions/<session-id>/` and Codex CLI logs at `~/.codex/sessions/...` as JSONL. **Not public APIs** — highest technical risk.

**Strategy:**
- **Tolerant decoder:** Parse known fields, capture entire record as `raw_payload` on unknown/malformed
- **Schema version detection:** Fingerprint records by field presence, track parser version per event
- **Graceful degradation:** Unknown record types → `action_type: "unknown"`, still audited
- **File discovery:** fsnotify on `~/.claude/projects/` and `~/.codex/sessions/` for new sessions; polling fallback (30s) if inotify limit exhausted
- **Tail strategy:** Track file offset per session file in SQLite; resume from last offset on restart
- **Drift detection:** Track `unknown_record_ratio` metric. If >50% unknown, emit `parser_drift` warning
- **Parser routing:** select parser by source path and record type (Claude Code vs Codex CLI)

**Event extraction targets:**

| CC Record Type | Maps To |
|---------------|---------|
| Tool call (Read, Write, Edit, Bash, etc.) | `tool_call` |
| File reads/writes | `file_access` |
| Shell command execution | `command_execution` |
| API request/response | `ai_request` |
| Unknown | `unknown` (raw payload preserved) |

### IPC Protocol

**JSON-RPC 2.0 over Unix socket** at `~/.local/share/crabwise/crabwise.sock`.

Why JSON-RPC: simple, well-specified, Go libraries exist, human-debuggable with socat.

```
Request/Response methods (standard JSON-RPC 2.0):
  status              → {agents, queue_depth, uptime, unclassified_tool_count}
  agents.list         → [{id, type, pid, adapter, status}]
  audit.query         → {events, total}  (accepts filters)
  audit.verify        → {valid, broken_at}
  audit.export        → {events}
  commandments.list   → [{name, enforcement, enabled}]
  commandments.test   → {evaluated, triggered}
  commandments.reload → {ok}

Subscription method (for `crabwise watch`):
  audit.subscribe     → long-lived NDJSON stream over the socket connection
                         Client sends JSON-RPC request with method "audit.subscribe"
                         and optional filter params. Server responds with initial ack,
                         then pushes NDJSON event lines until client disconnects.
                         Each line is a JSON-RPC notification (no id field).

Example frames:
  # client request
  {"jsonrpc":"2.0","id":1,"method":"audit.subscribe","params":{"agent":"claude-code"}}

  # server ack
  {"jsonrpc":"2.0","id":1,"result":{"ok":true,"stream":"audit"}}

  # server notification
  {"jsonrpc":"2.0","method":"audit.event","params":{"id":"evt_123","action_type":"tool_call","agent_id":"claude-code"}}
```

**Socket auth:**
- File permissions: `0700` on directory, `0600` on socket
- **Kernel-verified peer credentials:** `SO_PEERCRED` check on every connection — verify connecting PID's UID matches daemon UID. Reject mismatched UIDs.

### SQLite Schema (v2)

```sql
CREATE TABLE events (
    id              TEXT PRIMARY KEY,
    timestamp       TEXT NOT NULL,  -- RFC3339Nano
    agent_id        TEXT NOT NULL,
    agent_pid       INTEGER,
    action_type     TEXT NOT NULL,
    action          TEXT,
    arguments       TEXT,           -- JSON
    session_id      TEXT,
    working_dir     TEXT,
    parser_version  TEXT,
    outcome         TEXT NOT NULL,  -- success|failure|blocked|warned
    commandments_evaluated TEXT,    -- JSON array
    commandments_triggered TEXT,    -- JSON array
    provider        TEXT,
    model           TEXT,
    input_tokens    INTEGER,
    output_tokens   INTEGER,
    cost_usd        REAL,
    adapter_id      TEXT,
    adapter_type    TEXT,
    raw_payload_ref TEXT,           -- logical event-id ref to sidecar .zst blob
    hostname        TEXT,           -- v2: machine identity (os.Hostname)
    user_id         TEXT,           -- v2: kernel UID (os.Getuid), not $USER
    prev_hash       TEXT,
    event_hash      TEXT NOT NULL,
    redacted        INTEGER DEFAULT 0
);

CREATE INDEX idx_events_timestamp ON events(timestamp);
CREATE INDEX idx_events_agent_id ON events(agent_id);
CREATE INDEX idx_events_action_type ON events(action_type);
CREATE INDEX idx_events_outcome ON events(outcome);
CREATE INDEX idx_events_session_id ON events(session_id);

CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE file_offsets (
    file_path TEXT PRIMARY KEY,
    offset    INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE chain_anchors (
    epoch       TEXT PRIMARY KEY,  -- YYYY-MM-DD
    event_id    TEXT NOT NULL,
    event_hash  TEXT NOT NULL,
    created_at  TEXT NOT NULL
);
```

### Hash Chain

- **Algorithm:** SHA-256
- **Payload:** Canonical serialization of the **entire redacted event record** (all persisted fields except `event_hash` itself), deterministic field ordering, then `SHA256(canonical_bytes + prev_hash)`
- **Chain seed:** First event uses `prev_hash = "genesis"`
- **Serialization:** Single goroutine holds chain head; events serialized through it after concurrent processing
- **Corruption:** `--verify-integrity` reports first broken link and total chain length. No auto-repair — corruption is evidence.
- **Retention-aware integrity:** Chain uses daily epoch anchors. When retention GC prunes old events, it preserves the last event of each epoch as a segment anchor. `--verify-integrity` validates each segment independently, so pruning doesn't break verification UX. Anchors are stored in a `chain_anchors` table.

### Daemon Config

```yaml
# ~/.config/crabwise/config.yaml
daemon:
  socket_path: ~/.local/share/crabwise/crabwise.sock
  db_path: ~/.local/share/crabwise/crabwise.db
  raw_payload_dir: ~/.local/share/crabwise/raw/
  log_level: info          # debug|info|warn|error
  pid_file: ~/.local/share/crabwise/crabwise.pid

discovery:
  scan_interval: 10s
  process_signatures:
    - claude
    - codex
  log_paths:
    - ~/.claude/projects/
    - ~/.codex/sessions/

adapters:
  log_watcher:
    enabled: true
    poll_fallback_interval: 30s
  proxy:
    enabled: true
    listen: 127.0.0.1:9119
    upstream_timeout: 30s

queue:
  capacity: 10000
  batch_size: 100
  flush_interval: 1s
  overflow: block_with_timeout  # block_with_timeout|drop_oldest (throughput mode)
  block_timeout: 100ms

audit:
  retention_days: 30
  hash_algorithm: sha256
  raw_payload_max_size: 1048576   # 1MB per blob
  raw_payload_quota: 524288000    # 500MB total
  raw_payload_gc_interval: 1h

commandments:
  file: ~/.config/crabwise/commandments.yaml

otel:
  enabled: false
  endpoint: ""              # OTLP HTTP endpoint
  export_interval: 30s

cost:
  pricing:
    gpt-4o:        { input: 2.50, output: 10.00 }
    gpt-4o-mini:   { input: 0.15, output: 0.60 }
    claude-sonnet-4-5-20250929: { input: 3.00, output: 15.00 }
```

### Event Queue Design

```
Adapters (N goroutines) → [bounded channel, cap 10k] → serializer goroutine → [batch buffer] → SQLite writer

- Adapters send to buffered channel
- Serializer: reads from channel, assigns hash chain, adds to batch buffer
- Batch buffer flushes every 1s OR when batch_size reached
- On channel full: overflow policy (default: block_with_timeout for audit completeness; drop_oldest opt-in for throughput mode)
- Every drop increments atomic counter + emits pipeline_overflow synthetic event
- SQLite writer uses BEGIN/COMMIT for batch inserts
```

### Raw Payload Storage

- **Location:** `~/.local/share/crabwise/raw/` (0700 directory)
- **File permissions:** 0600 per blob
- **Naming:** `<event-id>.zst` — `RawPayloadRef` stores logical event ID, resolved to file path internally (prevents path injection)
- **Max blob size:** 1MB per payload (larger payloads truncated with marker)
- **Total quota:** 500MB default (configurable). GC runs hourly.
- **GC cadence:** Hourly sweep. Blobs older than `audit.retention_days` deleted. If quota exceeded, oldest blobs deleted first regardless of retention.
- **Compression:** zstd level 3 (fast compression, reasonable ratio)

### Proxy Egress Redaction

Redacting outbound payloads can alter model behavior. Deterministic ordering:

```
Request arrives at proxy
  → 1. Commandment evaluation (detect matches)
  → 2. Enforcement decision (block → reject immediately, warn → continue)
  → 3. Redaction (if commandment allows + redaction enabled for this path)
       - Only redact patterns explicitly configured (API keys, tokens, credentials)
       - Commandment-level override: `redact_egress: true|false` per rule
       - Audit marker: `egress_redacted: true` on event if redaction applied
  → 4. Forward to upstream (post-redaction payload)
```

### Filesystem Watching

- **Primary:** fsnotify (wraps inotify on Linux) on `~/.claude/projects/` and `~/.codex/sessions/`
- **Fallback:** If fsnotify watch limit exceeded, degrade to polling at configured interval
- **New sessions:** Watch for new directories, auto-add watches on session files
- **File rotation:** Detect truncation (offset > file size), reset offset to 0

---

## Risk Mitigations

### High Risk

**1. Claude Code log format instability**
- Tolerant decoder captures unknown fields as raw_payload without panicking
- Schema version fingerprinting tracks parser version per event
- Drift detection: if >50% unknown records, emit `parser_drift` warning
- Degradation: log watcher becomes less useful but never crashes; proxy adapter unaffected

**2. Hash chain serialization bottleneck**
- Only hash assignment is serial; input size is the full canonical redacted event record
- Parsing, commandment eval, batch buffering all concurrent
- Fallback: per-adapter chains with merge-verification (unlikely needed at prototype scale)

**3. SSE streaming correctness**
- Torture test suite built early in M2, not after proxy is "done"
- Tests: partial chunks, multi-event chunks, connection drops, client cancel, upstream timeout, empty events, malformed SSE
- Fallback: unhandled edge cases pass bytes through unmodified (transparent fallback)

### Medium Risk

**4. SQLite corruption / crash recovery**
- WAL mode. Batched writes in transactions.
- File offset tracking enables resume from last committed position
- Fallback: delete DB, restart — events re-parse from CC logs

**5. fsnotify / inotify limits**
- Poll fallback. Watch count tracked. Warning logged when approaching limit.
- `crabwise status` shows watch count and polling fallback state

**6. Proxy upstream unreachable**
- Clear error returned to agent with upstream status
- Logged as `outcome: failure` in audit
- Agent/user handles retry — proxy is transparent

---

## Testing Strategy

### Unit Tests (every milestone)
- Table-driven tests for all packages
- Commandment matchers: regex correctness, backtracking bounds, edge cases
- Hash chain: known-answer vectors, corruption detection
- Config parsing: valid, invalid, partial, defaults
- CC log parser: fixture-based contract tests

### Integration Tests (M0+)
- Daemon lifecycle: start/stop 100 cycles, no orphan processes, no leaked sockets
- Log watcher e2e: write synthetic JSONL → verify events in audit query
- IPC round-trip: CLI → socket → daemon → response

### Streaming Tests (M2)
- SSE torture suite via httptest:
  - Normal streaming
  - Partial chunks (split mid-event)
  - Multi-event chunks
  - Connection drop after N events
  - Client cancellation (context cancel)
  - Upstream timeout
  - Empty keep-alive events
  - Malformed SSE lines

### Benchmark Tests (M3 gate)
- Commandment eval latency (p95/p99)
- Proxy overhead (non-streaming, streaming first-token)
- SQLite batch insert throughput
- Memory footprint under dual-adapter load
- Queue saturation behavior

### Fixture Strategy
- `/testdata/claude-code/` — real CC log samples (anonymized)
- `/testdata/commandments/` — valid + invalid YAML files
- `/testdata/proxy/` — captured request/response pairs
- Fixtures checked into repo, CI breaks on parser regression

---

## Project Structure

```
crabwise/
├── cmd/
│   └── crabwise/
│       └── main.go
├── internal/
│   ├── daemon/
│   │   ├── daemon.go               # Lifecycle (start, run, stop)
│   │   └── config.go               # Config loading + validation
│   ├── adapter/
│   │   ├── adapter.go              # Adapter interface
│   │   ├── logwatcher/
│   │   │   ├── logwatcher.go       # Base log watcher (fsnotify + tail)
│   │   │   ├── claudecode.go       # CC JSONL parser
│   │   │   ├── codexcli.go         # Codex CLI JSONL parser
│   │   │   └── parser_router.go    # Source/type-based parser routing
│   │   └── proxy/
│   │       ├── proxy.go            # HTTP proxy server
│   │       ├── streaming.go        # SSE passthrough
│   │       └── openai.go           # OpenAI-compatible adapter
│   ├── classify/
│   │   ├── taxonomy.go             # Canonical category/effect definitions
│   │   └── registry.go             # Provider-aware registry + deterministic resolver
│   ├── commandments/
│   │   ├── schema.go               # YAML rule parser + validation
│   │   ├── engine.go               # Rule evaluator + reload
│   │   ├── matchers.go             # Pattern matching (regex, glob, numeric, list)
│   │   └── redaction.go            # Audit redaction pipeline
│   ├── audit/
│   │   ├── logger.go               # Batched SQLite writer + hash chain
│   │   ├── events.go               # Event type definitions
│   │   ├── query.go                # Query builder
│   │   ├── redaction.go            # Secret redaction pipeline
│   │   └── otel.go                 # OTel span emission
│   ├── discovery/
│   │   ├── scanner.go              # /proc + log file scanning
│   │   └── registry.go             # In-memory agent registry
│   ├── ipc/
│   │   ├── server.go               # Unix socket JSON-RPC server
│   │   └── client.go               # Client for CLI → daemon
│   ├── cli/
│   │   ├── root.go                 # Cobra root command
│   │   ├── start.go
│   │   ├── stop.go
│   │   ├── status.go
│   │   ├── watch.go                # Text streaming (M0) + Bubble Tea (M3)
│   │   ├── audit.go
│   │   ├── classify.go             # Dry-run taxonomy introspection
│   │   ├── commandments.go
│   │   └── agents.go
│   ├── queue/
│   │   └── queue.go                # Bounded event queue + metrics
│   └── store/
│       ├── sqlite.go               # Connection + migrations
│       └── migrations/
│           ├── 001_initial.sql
│           └── 002_add_origin.sql  # hostname + user_id columns
├── testdata/
│   ├── claude-code/                # CC log fixtures
│   ├── codex-cli/                  # Codex CLI log fixtures
│   ├── commandments/               # YAML test cases
│   └── proxy/                      # Request/response pairs
├── configs/
│   ├── default.yaml
│   ├── commandments_default.yaml
│   └── tool_registry.yaml
├── .github/
│   ├── workflows/
│   │   ├── ci.yml                  # Lint + test + build on push/PR
│   │   └── release.yml             # GoReleaser on tag push, gated on CI
│   └── dependabot.yml              # Weekly Go module + Actions updates
├── docs/
├── go.mod
├── go.sum
├── install.sh                      # curl | bash installer
├── Makefile
├── .golangci.yml                   # golangci-lint v2 config
├── .goreleaser.yml                 # GoReleaser: linux+darwin, amd64+arm64
├── LICENSE
└── README.md
```

---

## Verification Plan

1. **Agent discovery:** Start Claude Code/Codex CLI → `crabwise agents` shows both when active
2. **Log watcher audit:** Use Claude Code/Codex CLI normally → `crabwise audit` shows tool calls, commands, AI requests with token counts
3. **Parser safety:** Replay mixed-version JSONL fixtures → unknown records captured via raw_payload, no panics
4. **Commandment warn:** `protect-credentials` commandment → CC reads `.env` → `crabwise audit --triggered` shows warning
5. **Proxy block:** `OPENAI_BASE_URL=localhost:9119` → blocked model → denied, logged in audit
6. **Streaming torture:** SSE correct under chunking, disconnect, cancel, timeout
7. **Cost tracking:** `crabwise audit --cost` shows spend by agent/day
8. **Audit integrity:** `crabwise audit --verify-integrity` validates chain
9. **Redaction:** Secrets redacted in DB/OTel/CLI exports and proxy-forwarded payloads; original CC logs unchanged
10. **Performance:** Benchmark suite confirms all SLOs
11. **TUI:** `crabwise watch` shows live feed, warnings/blocks, queue depth, drop counters
12. **Classifier introspection:** `crabwise classify` confirms provider/default lookup path and reports `classification_source`
13. **Classifier drift signal:** `crabwise status` shows `unclassified_tool_count`; non-zero counts trigger taxonomy review

---

## De-scope Fallback List

If week-5 schedule slips, defer in this order (least critical first):

1. **TUI filters** (agent/action/trigger filtering in Bubble Tea) — basic unfiltered feed is sufficient
2. **OTel export** — local-only audit is the core value; OTel is optional enhancement
3. ~~**Install script** — manual binary download acceptable for prototype~~ ✅ Done in M0
4. ~~**Cross-compile arm64** — amd64 only is fine for prototype~~ ✅ Done in M0 (linux+darwin, amd64+arm64)
5. **`crabwise audit --cost`** — cost data still captured, just no summary view

**Never de-scope:** proxy correctness, SSE streaming, block enforcement, audit integrity, redaction.

---

## Key Differences from Original Plan

| Original | Revised |
|----------|---------|
| 6 milestones (M0-M4 + M0.5) | 4 vertical milestones (M0-M3) plus targeted M1.5 taxonomy hardening |
| M0.5 hardening before features | Hardening pieces moved to where their data lives |
| TUI deferred to M4 | Basic `watch` in M0, Bubble Tea in M3 |
| Commandments (M2) before proxy (M3) | Same order but proxy benefits from complete engine |
| No implementation specs | Concrete: SQL DDL, config YAML, IPC protocol, queue design |
| No risk mitigations | Explicit mitigations for top 6 risks |
| No testing strategy | Unit/integration/streaming/benchmark strategy with fixtures |
| No filesystem watching detail | fsnotify primary, polling fallback |
| No hash chain spec | SHA-256, serialization via single goroutine, genesis seed |

---

## Resolved Questions

1. **Go module path** — `github.com/crabwise-ai/crabwise`
2. **CC log format** — M0 gated on fixture capture + anonymization script. Must analyze real `~/.claude/` logs before parser development.
3. **Cost pricing** — config-driven static pricing in prototype. Manual update acceptable.
4. **OpenClaw adapter** — proxy-first only in prototype. No log watcher unless concrete user story appears.
5. **Systemd service** — optional via `crabwise install --service` (not default). Default: binary in PATH.
6. **License** — MIT. Single-player open source for devs. Team features (cloud sync, shared policies, push notifications) are the future commercial surface.

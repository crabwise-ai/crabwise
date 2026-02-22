# Crabwise AI — Prototype Implementation Design

## Context

Revision of the prototype architecture plan. Restructures milestones into vertical slices (each delivers a demo-able user story), adds concrete implementation specs, and addresses gaps/risks in the original plan.

**Canonical architecture:** `plan/crabwise-prototype-architecture-plan.md`
**This document:** Implementation-ready design with specs, risk mitigations, and testing strategy.

---

## What We're Building

Single Go binary (`crabwise`) — local-first daemon + CLI/TUI that monitors AI agent activity, enforces YAML-defined commandments at infrastructure level, and maintains a tamper-evident audit trail. Targets Claude Code (log watcher) and OpenAI-compatible agents (proxy) as first two adapters.

## Top User Stories

1. **As a dev**, I install Crabwise in one command and immediately see what Claude Code is doing — without changing any agent config.
2. **As a dev**, I define YAML rules like "never run rm -rf" enforced at infrastructure layer so agents can't bypass them via prompt injection.
3. **As a dev**, I get a searchable, exportable audit trail of every agent action — tool calls, file access, commands, AI requests — with cost tracking.
4. **As a security-conscious dev**, I get warnings when agents access sensitive files and can block destructive commands.

---

## Milestones (Vertical Slices)

### M0 — Foundation + First Value (Weeks 1-1.5)

**Demo:** `crabwise start && crabwise audit` shows Claude Code events

| Deliverable | Detail |
|------------|--------|
| Go scaffold | Cobra CLI, go.mod (`github.com/crabwise-ai/crabwise`), project structure |
| Daemon lifecycle | start/stop/status, PID file, signal handling (SIGTERM/SIGINT) |
| Config loading | `~/.config/crabwise/config.yaml` with sensible defaults |
| SQLite | `~/.local/share/crabwise/crabwise.db`, WAL mode, schema migrations |
| Agent discovery | /proc scanning for claude/openclaw processes, `~/.claude/` log detection |
| CC log watcher | Locate JSONL sessions via fsnotify, tail, parse into AuditEvents |
| Audit writer | Batched SQLite inserts, hash-chain integrity |
| CLI queries | `crabwise agents`, `crabwise audit` (time/agent/action filters), `--export json`, `--verify-integrity` |
| IPC | Unix socket (JSON-RPC 2.0), local-user-only permissions |
| Basic watch | `crabwise watch` — streaming text output (not TUI), live event tail |

**Exit gates:**
- Daemon starts/stops cleanly for 100 cycles, no orphans
- CC log events parsed and queryable in audit
- Hash chain validates end-to-end
- Zero parse panics on malformed/unknown records (captured via raw_payload)
- IPC socket permissions enforced (0700 dir, 0600 socket)
- `crabwise watch` streams events in real time

### M1 — Commandment Engine + Warn (Weeks 2-2.5)

**Demo:** `.env` access triggers warning in `crabwise audit --triggered`

| Deliverable | Detail |
|------------|--------|
| YAML parser | Commandment schema v1, validation |
| Pattern matching | Regex, glob, numeric comparisons, list membership |
| Precompiled matchers | Bounded execution, no catastrophic backtracking |
| Evaluation loop | Every event checked against active commandments |
| Warn enforcement | Flag in audit, surface in CLI output and watch |
| Commandment CLI | `list`, `add <file>`, `test <event>` (dry-run) |
| Starter pack | 5 default commandments (destructive cmds, credentials, spend, models, git push main) |
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
| Raw payloads | Sidecar `.zst` blobs at `~/.local/share/crabwise/raw/<event-id>.zst` |

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
| Release artifacts | `commandments.example.yaml`, README, MIT license |
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

---

## Implementation Specs

### Claude Code Log Parsing Strategy

CC logs at `~/.claude/projects/<project-hash>/sessions/<session-id>/` as JSONL. **Not a public API** — highest technical risk.

**Strategy:**
- **Tolerant decoder:** Parse known fields, capture entire record as `raw_payload` on unknown/malformed
- **Schema version detection:** Fingerprint records by field presence, track parser version per event
- **Graceful degradation:** Unknown record types → `action_type: "unknown"`, still audited
- **File discovery:** fsnotify on `~/.claude/projects/` for new sessions; polling fallback (30s) if inotify limit exhausted
- **Tail strategy:** Track file offset per session file in SQLite; resume from last offset on restart
- **Drift detection:** Track `unknown_record_ratio` metric. If >50% unknown, emit `parser_drift` warning

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
Methods:
  status              → {agents, queue_depth, uptime}
  agents.list         → [{id, type, pid, adapter, status}]
  audit.query         → {events, total}  (accepts filters)
  audit.stream        → server-push events (for watch)
  audit.verify        → {valid, broken_at}
  audit.export        → {events}
  commandments.list   → [{name, enforcement, enabled}]
  commandments.add    → {ok, errors}
  commandments.test   → {matches}
  commandments.reload → {ok}
```

Socket permissions: `0700` on directory, `0600` on socket. UID check on connection.

### SQLite Schema (v1)

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
    raw_payload_ref TEXT,           -- path to .zst sidecar
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
```

### Hash Chain

- **Algorithm:** SHA-256
- **Payload:** `SHA256(id + timestamp + agent_id + action_type + action + outcome + prev_hash)`
- **Chain seed:** First event uses `prev_hash = "genesis"`
- **Serialization:** Single goroutine holds chain head; events serialized through it after concurrent processing
- **Corruption:** `--verify-integrity` reports first broken link and total chain length. No auto-repair — corruption is evidence.

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
    - openclaw
  log_paths:
    - ~/.claude/projects/

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
  overflow: drop_oldest     # drop_oldest|block_with_timeout
  block_timeout: 100ms

audit:
  retention_days: 30
  hash_algorithm: sha256

commandments_file: ~/.config/crabwise/commandments.yaml

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
- On channel full: overflow policy (drop_oldest or block_with_timeout)
- Every drop increments atomic counter + emits pipeline_overflow synthetic event
- SQLite writer uses BEGIN/COMMIT for batch inserts
```

### Filesystem Watching

- **Primary:** fsnotify (wraps inotify on Linux) on `~/.claude/projects/`
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
- Only hash assignment is serial (sub-microsecond SHA-256 of ~200 bytes)
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
│   │   │   └── claudecode.go       # CC JSONL parser
│   │   └── proxy/
│   │       ├── proxy.go            # HTTP proxy server
│   │       ├── streaming.go        # SSE passthrough
│   │       └── openai.go           # OpenAI-compatible adapter
│   ├── commandments/
│   │   ├── engine.go               # Rule evaluator
│   │   ├── parser.go               # YAML rule parser
│   │   └── matchers.go             # Pattern matching (regex, glob)
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
│   │   ├── commandments.go
│   │   └── agents.go
│   ├── queue/
│   │   └── queue.go                # Bounded event queue + metrics
│   └── store/
│       ├── sqlite.go               # Connection + migrations
│       └── migrations/
│           └── 001_initial.sql
├── testdata/
│   ├── claude-code/                # CC log fixtures
│   ├── commandments/               # YAML test cases
│   └── proxy/                      # Request/response pairs
├── configs/
│   ├── default.yaml
│   └── commandments.example.yaml
├── scripts/
│   └── install.sh
├── plan/
├── docs/
├── go.mod
├── go.sum
├── Makefile
├── LICENSE
└── README.md
```

---

## Verification Plan

1. **Agent discovery:** Start Claude Code → `crabwise agents` shows it discovered
2. **Log watcher audit:** Use CC normally → `crabwise audit` shows tool calls, commands, AI requests with token counts
3. **Parser safety:** Replay mixed-version JSONL fixtures → unknown records captured via raw_payload, no panics
4. **Commandment warn:** `protect-credentials` commandment → CC reads `.env` → `crabwise audit --triggered` shows warning
5. **Proxy block:** `OPENAI_BASE_URL=localhost:9119` → blocked model → denied, logged in audit
6. **Streaming torture:** SSE correct under chunking, disconnect, cancel, timeout
7. **Cost tracking:** `crabwise audit --cost` shows spend by agent/day
8. **Audit integrity:** `crabwise audit --verify-integrity` validates chain
9. **Redaction:** Secrets redacted in DB/OTel/CLI exports and proxy-forwarded payloads; original CC logs unchanged
10. **Performance:** Benchmark suite confirms all SLOs
11. **TUI:** `crabwise watch` shows live feed, warnings/blocks, queue depth, drop counters

---

## Key Differences from Original Plan

| Original | Revised |
|----------|---------|
| 6 milestones (M0-M4 + M0.5) | 4 milestones (M0-M3), each a vertical slice |
| M0.5 hardening before features | Hardening pieces moved to where their data lives |
| TUI deferred to M4 | Basic `watch` in M0, Bubble Tea in M3 |
| Commandments (M2) before proxy (M3) | Same order but proxy benefits from complete engine |
| No implementation specs | Concrete: SQL DDL, config YAML, IPC protocol, queue design |
| No risk mitigations | Explicit mitigations for top 6 risks |
| No testing strategy | Unit/integration/streaming/benchmark strategy with fixtures |
| No filesystem watching detail | fsnotify primary, polling fallback |
| No hash chain spec | SHA-256, serialization via single goroutine, genesis seed |

---

## Unresolved Questions

1. **Go module path** — `github.com/crabwise-ai/crabwise`? Different org?
2. **CC log format** — need to analyze real CC logs before M0 to validate parser assumptions. Have access to `~/.claude/` on dev machine?
3. **Cost pricing source** — hardcoded config for prototype. Acceptable or need API-driven?
4. **OpenClaw adapter** — proxy-first per plan. Any need for OpenClaw log watcher in prototype?
5. **Systemd service** — install script creates user service? Or just binary in PATH?
6. **License** — MIT confirmed?

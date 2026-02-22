# M0 Implementation Plan: Foundation + First Value

## Context

Crabwise is a local-first Go daemon + CLI that monitors AI agent activity. No code exists yet — pure greenfield. This plan covers M0 only: daemon lifecycle, CC log watcher, SQLite audit trail, basic CLI.

## Top User Stories

1. `crabwise start` — daemon runs, discovers Claude Code sessions automatically
2. `crabwise audit` — query structured, hash-chained event history with filters
3. `crabwise watch` — stream events live as Claude Code generates them
4. `crabwise audit --verify-integrity` — prove no events tampered with
5. `crabwise agents` — see discovered AI agents and their status

## Module & Dependencies

- Module: `github.com/crabwise-ai/crabwise`
- `github.com/spf13/cobra` — CLI
- `gopkg.in/yaml.v3` — config
- `modernc.org/sqlite` — pure-Go SQLite
- `github.com/fsnotify/fsnotify` — filesystem watching
- `github.com/google/uuid` — event IDs
- `golang.org/x/sys` — SO_PEERCRED

## Design Decisions (from feedback)

1. **Subagents**: separate `agent_id` with `parent_session_id` link
2. **Default config**: `go:embed`, write on explicit `crabwise init` (not silently)
3. **Daemon mode**: foreground-only in M0 (`crabwise start` runs foreground; user manages backgrounding via systemd/&)
4. **Redaction in M0**: raw payload persistence disabled by default (`raw_payload_enabled: false`); no raw blobs stored until M1 adds redaction
5. **Hash canonicalization**: fixed struct field order, not JSON sorted keys — deterministic serializer enumerates fields in declared order
6. **Queue overflow**: separate control channel for `pipeline_overflow` events (never sent through the main queue)
7. **fsnotify**: explicit per-directory watches (not recursive); walk on startup, watch new dirs via parent CREATE events
8. **`audit.subscribe` contract**: defined below

### `audit.subscribe` Wire Format

```
# Client request
{"jsonrpc":"2.0","id":1,"method":"audit.subscribe","params":{"agent":"claude-code"}}

# Server ack (immediate)
{"jsonrpc":"2.0","id":1,"result":{"ok":true}}

# Server notifications (NDJSON, one per line)
{"jsonrpc":"2.0","method":"audit.event","params":{<AuditEvent fields>}}

# Server heartbeat (every 30s if no events)
{"jsonrpc":"2.0","method":"audit.heartbeat","params":{"ts":"2026-02-22T14:00:00Z"}}

# Lifecycle: client disconnects → server cleans up subscriber channel
# No reconnect/resume protocol in M0 — client re-subscribes from scratch
```

---

## Execution Phases

### Phase 0: Bootstrap (sequential, single agent)

**Task A1: Go scaffold + shared event contract**
- `go.mod`, `cmd/crabwise/main.go`, `internal/cli/root.go`, `Makefile`, `configs/default.yaml`, `LICENSE`
- **`internal/audit/events.go`** — shared event contract (AuditEvent struct, ActionType/Outcome constants, canonical serializer with fixed field order) — created here so all streams share the same types
- Cobra root command with version flag
- Makefile: `build`, `test` (`go test -race ./...`), `vet`, `lint` targets
- All deps added to go.mod upfront (prevents merge conflicts)

### Phase 1: Three parallel subagent streams

After scaffold merges, three streams work on disjoint file sets.

#### Stream 1 — Storage + Event Pipeline
Files: `internal/audit/` (except events.go), `internal/store/`, `internal/queue/`

**Task B1: SQLite store** — `internal/store/sqlite.go`, `internal/store/migrations/001_initial.sql`
- Open with WAL mode, run migrations
- `InsertEvents([]AuditEvent)` batch insert
- `GetFileOffset` / `SetFileOffset` for log resume
- Schema from spec (events, schema_version, file_offsets, chain_anchors)

**Task B2: Query builder** — `internal/audit/query.go`
- `QueryFilter` with since/until/agent/action/session/outcome/limit/offset
- `QueryEvents`, `ExportJSON`

**Task C1: Bounded event queue** — `internal/queue/queue.go`
- Buffered channel (cap 10k default)
- `block_with_timeout` (default 100ms) and `drop_oldest` overflow policies
- Atomic drop/overflow counters
- Separate control channel for `pipeline_overflow` synthetic events (avoids deadlock)
- `Stats()` for monitoring

**Task C2: Audit logger + hash chain** — `internal/audit/logger.go`
- Single serializer goroutine: reads queue, computes SHA-256 hash, batches, flushes
- Hash: `SHA256(canonical_bytes(event, fixed_field_order) + prev_hash)`, genesis = `"genesis"`
- Canonical serializer: enumerates AuditEvent fields in declared struct order, deterministic byte output
- Batch flush on timer (1s) or size (100)
- Daily epoch anchors in `chain_anchors` table
- `VerifyIntegrity()` — validates chain segments, reports first broken link
- `Subscribe()` / `Unsubscribe()` — channels for watch streaming

#### Stream 2 — CC Parser + Log Watcher
Files: `internal/adapter/`, `testdata/`, `scripts/`

**Task E0: CC log fixtures** — `testdata/claude-code/`
- Capture real CC log samples from `~/.claude/projects/`
- Anonymize: replace paths, UUIDs, user content; preserve structure/types/tool names
- Fixture files: `session-basic.jsonl`, `session-toolheavy.jsonl`, `session-malformed.jsonl`, `session-empty.jsonl`, plus single-record files per type

**Task E1: CC log parser** — `internal/adapter/logwatcher/claudecode.go`
- Tolerant decoder: parse known fields, capture entire record on unknown/malformed
- Record type mapping:
  - `assistant` + `tool_use` → `tool_call` (Bash→`command_execution`, Read/Write/Edit/Glob/Grep→`file_access`)
  - `assistant` + `usage` → `ai_request` (model, tokens)
  - `user` + `tool_result` → correlate with tool call via `tool_use_id`
  - `progress` → informational, minimal audit
  - `file-history-snapshot`, `queue-operation`, `summary` → skip
  - `system` → `action_type: "system"`
  - Unknown → `action_type: "unknown"`, raw payload preserved in-memory (not persisted in M0)
- Subagent files: separate `agent_id`, link via `parent_session_id`
- Schema version fingerprinting, parser version tracking
- Drift detection: >50% unknown → `parser_drift` warning

**Task E2: Log watcher** — `internal/adapter/logwatcher/logwatcher.go`, `internal/adapter/adapter.go`
- `Adapter` interface: `Start(ctx, chan<- *AuditEvent)`, `Stop()`, `CanEnforce() bool`
- fsnotify: explicit per-directory watches on `~/.claude/projects/`; walk dirs on startup, watch parent for new subdirs
- Tail from last offset (stored in SQLite `file_offsets`), parse each JSONL line
- Polling fallback (30s) when inotify limit exhausted — test this path explicitly
- File truncation detection (offset > size → reset)

#### Stream 3 — IPC + Config + Discovery
Files: `internal/daemon/config.go`, `internal/discovery/`, `internal/ipc/`

**Task A2: Config loading** — `internal/daemon/config.go`
- Full `Config` struct matching spec YAML
- Defaults via `go:embed` of `configs/default.yaml`
- `~/.config/crabwise/config.yaml` overrides if present
- `~` expansion, XDG-aware paths
- Validation: required fields, enum values, numeric bounds
- `raw_payload_enabled: false` default in M0

**Task D1: Agent discovery** — `internal/discovery/scanner.go`, `internal/discovery/registry.go`
- `/proc/*/cmdline` scan for `claude` processes
- `~/.claude/projects/` scan for session JSONL files
- `Registry`: in-memory map, `Update()`, `List()`, `Get()`
- Periodic re-scan (every 10s via config)

**Task F1: IPC server** — `internal/ipc/server.go`
- JSON-RPC 2.0 over Unix socket
- Socket dir `0700`, socket `0600`
- `SO_PEERCRED` on every connection — reject UID mismatch
- Negative test: verify rejection on UID mismatch
- Methods: `status`, `agents.list`, `audit.query`, `audit.verify`, `audit.export`, `audit.subscribe`
- `audit.subscribe`: ack → NDJSON notifications → heartbeat every 30s → cleanup on disconnect

**Task F2: IPC client** — `internal/ipc/client.go`
- `Dial(socketPath)`, `Call(method, params)`, `Subscribe(method, params)`

### Phase 2: Integration (sequential, merges all streams)

**Task G1: Daemon lifecycle** — `internal/daemon/daemon.go`
- Startup: PID file → config → SQLite → queue → audit logger → IPC server → discovery → adapters
- Shutdown: cancel ctx → stop adapters → drain queue → close IPC → close SQLite → remove PID
- Signal handling: SIGTERM/SIGINT → graceful shutdown
- Foreground-only in M0 (no re-exec daemonization)

**Task H1: start/stop/status CLI** — `internal/cli/start.go`, `stop.go`, `status.go`
- `start`: runs daemon in foreground (user manages backgrounding)
- `stop`: read PID file, send SIGTERM, wait
- `status`: IPC `status` call, format output

**Task H2: agents CLI** — `internal/cli/agents.go`
- IPC `agents.list`, table output

**Task H3: audit CLI** — `internal/cli/audit.go`
- Flags: `--since`, `--until`, `--agent`, `--action`, `--session`, `--limit`, `--export json`, `--verify-integrity`

**Task H4: watch CLI** — `internal/cli/watch.go`
- IPC `audit.subscribe`, streaming text output (not TUI)
- Format: `14:23:05 [claude-code] tool_call  Read  src/main.ts`

---

## File Ownership (no merge conflicts)

| Stream | Directories |
|--------|------------|
| 0 (Bootstrap) | `go.mod`, `cmd/`, `internal/cli/root.go`, `internal/audit/events.go`, `Makefile`, `configs/`, `LICENSE` |
| 1 (Storage+Pipeline) | `internal/audit/logger.go,query.go`, `internal/store/`, `internal/queue/` |
| 2 (Parser+Watcher) | `internal/adapter/`, `testdata/`, `scripts/` |
| 3 (IPC+Config+Discovery) | `internal/daemon/config.go`, `internal/discovery/`, `internal/ipc/` |
| Integration | `internal/daemon/daemon.go`, `internal/cli/start,stop,status,agents,audit,watch.go` |

---

## Exit Gates

- [ ] Daemon starts/stops cleanly for 100 cycles, no orphans
- [ ] CC log events parsed and queryable in audit
- [ ] Hash chain validates end-to-end
- [ ] Zero parse panics on malformed/unknown records
- [ ] IPC socket permissions enforced (0700 dir, 0600 socket, SO_PEERCRED verified)
- [ ] SO_PEERCRED UID mismatch rejected (negative test)
- [ ] `crabwise watch` streams events in real time
- [ ] `go test -race ./...` passes (mandatory gate)
- [ ] inotify fallback path tested

## Testing Strategy

- Table-driven unit tests for all packages
- Fixture-based CC parser tests (`testdata/claude-code/`)
- Integration tests: daemon lifecycle (100 cycles), IPC round-trip, log watcher e2e
- `go test -race ./...` mandatory in Makefile `test` target
- Hash chain known-answer vectors with fixed field order serializer
- Negative tests: SO_PEERCRED UID mismatch, inotify fallback, malformed JSON-RPC

## Verification

1. `go build ./...` succeeds
2. `crabwise start` runs in foreground, `crabwise status` (from another terminal) shows running daemon
3. Open Claude Code → `crabwise agents` shows it discovered
4. Use Claude Code → `crabwise audit` shows tool calls, AI requests with tokens
5. `crabwise watch` shows live event stream
6. `crabwise audit --verify-integrity` passes
7. `crabwise audit --export json` produces valid JSON
8. Kill daemon → `crabwise start` → events resume from offset (no duplicates)
9. Feed malformed JSONL → no panics, unknown records captured
10. `crabwise stop` → clean exit, no orphan processes

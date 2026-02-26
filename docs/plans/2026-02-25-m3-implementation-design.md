# M3 Implementation Design (Core Gates First) — ✅ COMPLETE

## Status

**Merged:** PR #12 (`feat/m3-core-gates-execution`) — 2026-02-26

All core-gates-first deliverables shipped. Remaining M3 table items (OTel, install script, cross-compile, advanced TUI filters, sustained-load SLOs) are tracked in M3.5.

## Context

This design defines the approved M3 execution strategy for Crabwise after M2 completion. It prioritizes release gates and operational reliability first, then delivers a minimal Bubble Tea `watch` experience, and finally completes release polish.

Decision summary:
- Optimize for core gates first.
- Keep Bubble Tea scope minimal for first pass.
- Remove OTel from M3 scope.
- Add daemon-level proxy E2E smoke coverage as a release gate.
- Keep text-mode watch fallback (`--text`) for headless/CI usage.

## Recommended Approach

We use a phased approach:

1. Phase A: CI-enforced quality gates and risk reduction.
2. Phase B: Minimal Bubble Tea TUI for `crabwise watch`.
3. Phase C: Release polish and artifact/documentation readiness.

This balances delivery speed with confidence and avoids turning M3 into an open-ended performance lab.

## Scope and Non-Goals

### In Scope

- ✅ CI benchmark gate for commandment evaluation latency and proxy latency/first-token overhead.
- ✅ Daemon-level forward proxy E2E smoke test through real startup/runtime wiring.
- ✅ Bubble Tea single-screen watch UI with three regions:
  - live event feed from `audit.subscribe`
  - status strip (queue depth, dropped count, uptime) — polled via IPC every 3s
  - trigger-rate counter (rolling 1-minute window from streamed events)
- ✅ Release readiness polish for artifacts/docs.

### Out of Scope (M3)

- OTel implementation (moved out of M3).
- Interactive TUI filters, scrollback UX, mouse support.
- Full sustained-load rig (100 events/sec, long-window RSS tracking) as a hard gate.

### Deferred to M3.5

The following items from the original M3 table in the prototype plan were not part of the core-gates-first scope and are tracked in M3.5:

- OTel export (GenAI span emission, optional collector)
- Install script (`curl -fsSL ... | sh`, platform detection, checksum verification)
- Cross-compile (Linux amd64 + arm64)
- Advanced TUI features (filter by agent/action/triggers, warning/block indicators)
- Sustained-load SLO confirmation:
  - RSS < 80MB under dual-adapter active load
  - Event loss = 0 under nominal load with overflow metering validation
  - SQLite batch insert throughput characterization under sustained load
- Full reproducibility benchmark profile (10 concurrent clients, 4KB/16KB payloads, 10s warmup + 60s measure)
- Binary verification documented in installer
- Install script works on fresh Ubuntu + Arch

## Architecture and Sequencing

### Phase A - Gates First ✅

1. ✅ Benchmark harness runnable in CI (`scripts/ci/check_m3_bench.sh`, `make bench-gate`, CI `benchmark-gate` job).
2. ✅ p95/p99 regression thresholds enforced for:
   - commandment eval latency (p95 < 2ms, p99 < 8ms) — `TestEvalLatencySLO`
   - proxy round-trip overhead (p95 < 20ms) — `TestProxyLatencyGate`
   - proxy first-token delta (p95 < 50ms) — `TestProxyFirstTokenGate`
   - all gates emit p50/p95/p99/max output
3. ✅ Daemon-level proxy E2E smoke with deterministic mock upstream — `TestDaemonProxyE2E_AllowPath`, `TestDaemonProxyE2E_BlockPath`
4. ✅ Unix socket path stabilization for macOS test reliability
5. ✅ Proxy `Start()`/`Stop()` data race fix (`httpSrvMu` guard)

Measurement note:
- CI gate uses a reduced, deterministic profile for regression detection.
- Full benchmark profile from the prototype plan (10 concurrent clients, 4KB/16KB payloads, 10s warmup + 60s measurement) remains the reproducibility profile for M3.5 benchmarking.

Design intent: validate the highest-risk release behaviors quickly and fail fast in CI.

### Phase B - Minimal Bubble Tea Watch ✅

1. ✅ Bubble Tea single-screen model as default watch view (`internal/cli/watch_tui.go`).
2. ✅ Presentation is read-only and lightweight.
3. ✅ Events from `audit.subscribe` stream + `status` polled via short-lived IPC every 3s (with `OK` flag to avoid zeroing metrics on poll failure).
4. ✅ Trigger-rate computed in-process from streamed events (rolling 1-minute window, counts warned/blocked outcomes + non-empty `commandments_triggered`).
5. ✅ Text fallback via `crabwise watch --text` (`internal/cli/watch.go` with signal-safe shutdown).

Design intent: hit the M3 demo target with low complexity and high reliability.

### Phase C - Release Polish ✅ (partial)

1. ✅ Release artifacts and docs aligned with shipped behavior (README, prototype plan, `m3-plan-and-tasks.md`).
2. Installer verification guidance → M3.5 (no install script yet).
3. ✅ OTel confirmed as M3.5 work.

## Data Flow

### Benchmark Gate Flow

`go test -bench` -> benchmark output parser -> threshold evaluation -> CI pass/fail.

Required output includes `p50/p95/p99/max` for each gated metric. ✅ Implemented.

### Proxy E2E Smoke Flow

`crabwise init` -> daemon start -> forward proxy request via MITM path -> mock upstream -> audit/IPC assertions.

Two required paths:
- allow path: request reaches upstream and audit records expected request event.
- block path: upstream receives zero requests and audit records `outcome=blocked` with trigger metadata.

### Bubble Tea Watch Flow

- Subscribe to `audit.subscribe` for live event feed.
- Poll `status` for queue depth, dropped count, uptime.
- Update trigger-rate counter from event stream in a rolling window.

## Error Handling and Resilience

### Benchmarks

- Threshold breach fails CI with explicit metric and observed values.
- No retry masking in benchmark gate.

### Proxy E2E Smoke

- Fail with hop-specific errors (startup, connect, TLS, forward, audit assertion).
- Ensure blocked-path assertion includes both upstream zero-hit and blocked audit evidence.
- Force short runtime paths in tests to avoid Unix socket path-length failures on temp directories.

### TUI Runtime

- On stream disconnect, attempt one reconnect after short backoff (1-2 seconds).
- If reconnect fails, exit with actionable error message.

## Testing and Exit Gates

M3 sign-off requires:

1. ✅ CI benchmark gate is green for defined latency thresholds.
2. ✅ CI daemon-level proxy E2E smoke is green for both allow and block cases.
3. ✅ Bubble Tea minimal dashboard is functional and stable.
4. ✅ Existing test suites remain green after integration (`go test -race -count=1 ./...` — 11/11 packages).
5. ✅ Deferred SLO items tracked explicitly in M3.5 (not implicit pass criteria for M3 CI gate).

## Implementation Slices

### Slice A1 - CI Benchmark Gate ✅

- ✅ Benchmark targets and threshold checks.
- ✅ Integrated into CI as authoritative gate.

### Slice A2 - Proxy E2E Smoke ✅

- ✅ Deterministic daemon-level integration test using mock upstream.
- ✅ Allow and block behavior asserted with audit confirmation.

### Slice B1 - Bubble Tea Minimal Watch ✅

- ✅ Single-screen watch UI.
- ✅ Event stream + status polling (3s IPC) + trigger-rate counter.
- ✅ One reconnect attempt with 1.5s backoff.
- ✅ Text fallback mode via `--text` for non-TTY/headless usage.

### Slice C2 - Deferred SLO Follow-up → M3.5

- Deferred to M3.5: RSS, event-loss, and SQLite sustained-load benchmarks.
- Did not block M3 core release gate.

### Slice C1 - Release Polish ✅

- ✅ Docs/artifacts aligned with shipped behavior.

## De-scope Order (If Schedule Slips)

1. Advanced TUI interactions (filters/scrollback/mouse).
2. Extended load-lab and long-window RSS harness.
3. OTel remains out of M3 by default.

## Unplanned Additions (during implementation)

- **Status polling resilience**: `statusResultMsg.OK` flag prevents transient IPC failures from zeroing dashboard metrics.
- **Proxy data race fix**: `httpSrvMu` mutex guards `Proxy.httpSrv` against concurrent `Start()`/`Stop()` access.
- **Unix socket path stabilization**: `newTestRuntimePaths` / `shortRuntimeDir` helpers with `/tmp` fallback for macOS 104-byte socket limit.
- **Benchmark p50/max output**: All gates emit p50/p95/p99/max (design doc originally specified this; implementation initially only had p95/p99).

## Approval Record

This design was reviewed and approved in collaborative discussion before implementation planning.
Implementation completed and merged via PR #12 on 2026-02-26.

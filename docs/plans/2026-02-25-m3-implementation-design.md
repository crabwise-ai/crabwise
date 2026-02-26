# M3 Implementation Design (Core Gates First)

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

- CI benchmark gate for commandment evaluation latency and proxy latency/first-token overhead.
- Daemon-level forward proxy E2E smoke test through real startup/runtime wiring.
- Bubble Tea single-screen watch UI with three regions:
  - live event feed from `audit.subscribe`
  - status strip (queue depth, dropped count, uptime)
  - trigger-rate counter
- Release readiness polish for artifacts/docs.

### Out of Scope (M3)

- OTel implementation (moved out of M3).
- Interactive TUI filters, scrollback UX, mouse support.
- Full sustained-load rig (100 events/sec, long-window RSS tracking) as a hard gate.

### Deferred SLO Coverage (Follow-up Track)

The prototype M3 SLO table includes additional release-gate measurements that are not part of the core-gates-first CI profile.

- Deferred to follow-up benchmark track (`M3.1`):
  - RSS < 80MB under dual-adapter active load
  - Event loss = 0 under nominal load with overflow metering validation
  - SQLite batch insert throughput characterization under sustained load
- M3 CI gate remains authoritative for: commandment eval latency, proxy overhead/first-token delta, and daemon-level proxy allow/block E2E correctness.

## Architecture and Sequencing

### Phase A - Gates First

1. Add benchmark harness runnable in CI.
2. Measure and enforce p95/p99 regression thresholds for:
   - commandment eval latency
   - proxy round-trip and first-token overhead
3. Add daemon-level proxy E2E smoke test with deterministic mock upstream.

Measurement note:
- CI gate uses a reduced, deterministic profile for regression detection.
- Full benchmark profile from the prototype plan (10 concurrent clients, 4KB/16KB payloads, 10s warmup + 60s measurement) remains the reproducibility profile for follow-up benchmarking, not this first M3 CI gate.

Design intent: validate the highest-risk release behaviors quickly and fail fast in CI.

### Phase B - Minimal Bubble Tea Watch

1. Add Bubble Tea single-screen model as default watch view.
2. Keep presentation read-only and lightweight.
3. Pull events from `audit.subscribe` and poll `status` periodically.
4. Compute trigger-rate in-process from streamed events.
5. Keep existing text stream as explicit fallback via `crabwise watch --text`.

Design intent: hit the M3 demo target with low complexity and high reliability.

### Phase C - Release Polish

1. Confirm release artifacts and docs are consistent with shipped behavior.
2. Ensure installer verification guidance is documented.
3. Leave OTel as future milestone work.

## Data Flow

### Benchmark Gate Flow

`go test -bench` -> benchmark output parser -> threshold evaluation -> CI pass/fail.

Required output includes `p50/p95/p99/max` for each gated metric.

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

1. CI benchmark gate is green for defined latency thresholds.
2. CI daemon-level proxy E2E smoke is green for both allow and block cases.
3. Bubble Tea minimal dashboard is functional and stable.
4. Existing test suites remain green after integration.
5. Deferred SLO items are tracked explicitly as follow-up work and not treated as implicit pass criteria for this M3 CI gate.

## Implementation Slices

### Slice A1 - CI Benchmark Gate

- Add benchmark targets and threshold checks.
- Integrate into CI as authoritative gate.

### Slice A2 - Proxy E2E Smoke

- Add deterministic daemon-level integration test using mock upstream.
- Assert allow and block behavior with audit confirmation.

### Slice B1 - Bubble Tea Minimal Watch

- Build single-screen watch UI.
- Wire event stream + status polling + trigger-rate counter.
- Add one reconnect attempt with short backoff.
- Preserve text fallback mode via `--text` for non-TTY/headless usage.

### Slice C2 - Deferred SLO Follow-up Stub (M3.1)

- Add tracking issue/plan section for RSS, event-loss, and SQLite sustained-load benchmarks.
- Do not block M3 core release gate on this slice.

### Slice C1 - Release Polish

- Align docs/artifacts/install verification with shipped behavior.

## De-scope Order (If Schedule Slips)

1. Advanced TUI interactions (filters/scrollback/mouse).
2. Extended load-lab and long-window RSS harness.
3. OTel remains out of M3 by default.

## Approval Record

This design was reviewed and approved in collaborative discussion before implementation planning.

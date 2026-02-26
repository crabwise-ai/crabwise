# M3 Implementation Design (Core Gates First)

## Context

This design defines the approved M3 execution strategy for Crabwise after M2 completion. It prioritizes release gates and operational reliability first, then delivers a minimal Bubble Tea `watch` experience, and finally completes release polish.

Decision summary:
- Optimize for core gates first.
- Keep Bubble Tea scope minimal for first pass.
- Remove OTel from M3 scope.
- Add daemon-level proxy E2E smoke coverage as a release gate.

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

## Architecture and Sequencing

### Phase A - Gates First

1. Add benchmark harness runnable in CI.
2. Measure and enforce p95/p99 regression thresholds for:
   - commandment eval latency
   - proxy round-trip and first-token overhead
3. Add daemon-level proxy E2E smoke test with deterministic mock upstream.

Design intent: validate the highest-risk release behaviors quickly and fail fast in CI.

### Phase B - Minimal Bubble Tea Watch

1. Replace text watch path with Bubble Tea single-screen model.
2. Keep presentation read-only and lightweight.
3. Pull events from `audit.subscribe` and poll `status` periodically.
4. Compute trigger-rate in-process from streamed events.

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

### TUI Runtime

- On stream disconnect, attempt one reconnect after short backoff (1-2 seconds).
- If reconnect fails, exit with actionable error message.

## Testing and Exit Gates

M3 sign-off requires:

1. CI benchmark gate is green for defined latency thresholds.
2. CI daemon-level proxy E2E smoke is green for both allow and block cases.
3. Bubble Tea minimal dashboard is functional and stable.
4. Existing test suites remain green after integration.

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

### Slice C1 - Release Polish

- Align docs/artifacts/install verification with shipped behavior.

## De-scope Order (If Schedule Slips)

1. Advanced TUI interactions (filters/scrollback/mouse).
2. Extended load-lab and long-window RSS harness.
3. OTel remains out of M3 by default.

## Approval Record

This design was reviewed and approved in collaborative discussion before implementation planning.

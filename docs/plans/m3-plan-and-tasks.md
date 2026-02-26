# M3 Plan and Tasks (Core Gates First) — ✅ COMPLETE

**Merged:** PR #12 (`feat/m3-core-gates-execution`) — 2026-02-26

## Scope

Deliver M3 by prioritizing release-confidence gates before optional expansion work.

## Task List

- ✅ **A1 — Latency gates in CI:** commandment eval (p95 < 2ms, p99 < 8ms), proxy roundtrip (p95 < 20ms), first-token delta (p95 < 50ms). All emit p50/p95/p99/max. CI `benchmark-gate` job + `make bench-gate`.
- ✅ **A2 — Daemon proxy E2E smoke:** `TestDaemonProxyE2E_AllowPath` + `TestDaemonProxyE2E_BlockPath` with full daemon+proxy runtime, generated CA, mock upstream, audit assertions.
- ✅ **B1 — Minimal watch UX:** Bubble Tea dashboard (feed + status strip + trigger-rate), status polling via IPC every 3s, single reconnect, `--text` fallback.
- ✅ **C1 — Release polish/docs:** README, prototype plan, and this doc aligned to core-gates-first scope.
- **C2 → M3.5:** Sustained-load benchmark track (RSS, event-loss, SQLite throughput), OTel, install script, cross-compile, advanced TUI filters.

## Explicit Non-Goals (M3)

- ✅ OpenTelemetry export/sign-off confirmed out of M3 scope → M3.5.
- ✅ Advanced watch filtering/interactions confirmed out of M3 scope → M3.5.

## Sign-Off Gates

- ✅ Commandment eval latency gate passes with stable `m3_bench` percentile output.
- ✅ Proxy roundtrip and first-token gates pass with stable `m3_bench` output.
- ✅ Daemon proxy E2E smoke passes for both allow and block behavior.
- ✅ Minimal watch behavior validated, including one-shot reconnect behavior and `--text` fallback path.
- ✅ Docs and release notes reflect core-gates-first execution and C2 deferred SLO follow-up.

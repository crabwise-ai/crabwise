# M3 Plan and Tasks (Core Gates First)

## Scope

Deliver M3 by prioritizing release-confidence gates before optional expansion work.

## Task List

- **A1 — Latency gates in CI:** enforce commandment eval and proxy latency/first-token gate outputs in the reduced deterministic CI profile.
- **A2 — Daemon proxy E2E smoke:** require allow-path and block-path tests at daemon level, including audit/outcome assertions.
- **B1 — Minimal watch UX:** ship Bubble Tea watch with live feed + status + trigger-rate visibility and retain `--text` fallback.
- **C1 — Release polish/docs:** align README and plan docs to core-gates-first scope and deferred benchmark language.
- **C2 (deferred) — Sustained-load benchmark track:** run full benchmark profile and confirm RSS, event-loss, and SQLite throughput gates.

## Explicit Non-Goals (M3)

- OpenTelemetry export/sign-off is out of M3 must-ship scope.
- Advanced watch filtering/interactions are out of M3 must-ship scope.

## Sign-Off Gates

- Commandment eval latency gate passes with stable `m3_bench` percentile output.
- Proxy roundtrip and first-token gates pass with stable `m3_bench` output.
- Daemon proxy E2E smoke passes for both allow and block behavior.
- Minimal watch behavior is validated, including one-shot reconnect behavior and `--text` fallback path.
- Docs and release notes reflect core-gates-first execution and C2 deferred SLO follow-up.

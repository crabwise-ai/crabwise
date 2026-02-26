# M3 Core Gates First Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship M3 with release-confidence gates first (bench + daemon-level proxy E2E), then deliver a minimal Bubble Tea `watch` UI with a text fallback, and finish release polish without adding OTel scope.

**Architecture:** Keep the existing daemon/proxy/audit architecture intact and add thin, test-first slices: (1) CI quality gates, (2) minimal TUI presentation layer, (3) docs/release alignment. Prefer deterministic local mock integrations over environment-dependent tests. Keep M3 non-essential features (OTel, advanced TUI interactions) out of the critical path.

**SLO Scope Note:** This plan intentionally uses a reduced CI regression profile for fast pass/fail gating. Full reproducibility profile benchmarking (10 concurrent clients, 4KB/16KB payloads, 10s warmup + 60s measure) and sustained-load SLOs (RSS/event-loss/SQLite throughput) are explicitly deferred to a follow-up track and must be documented as deferred in milestone docs.

**Tech Stack:** Go 1.25, Cobra CLI, Bubble Tea/Bubbles/Lip Gloss, GitHub Actions CI, existing Crabwise daemon/proxy/audit packages.

---

### Task 0: Stabilize Unix Socket Test Paths (Precondition)

**Files:**
- Modify: `internal/cli/audit_integration_test.go`
- Modify: `internal/cli/status_test.go`
- Modify: `internal/daemon/daemon_test.go` (shared helper if reused)
- Test: `internal/cli/audit_integration_test.go`, `internal/cli/status_test.go`

**Step 1: Write/confirm failing tests with current long temp paths**

Run: `go test ./internal/cli -run 'Test(StatusCommand_ShowsUnclassifiedToolCount|AuditCodexAgentEndToEnd)' -count=1 -v`
Expected: FAIL on macOS with `listen unix ... bind: invalid argument` when socket path exceeds platform limits.

**Step 2: Add short-path helper for runtime files**

```go
func shortRuntimePath(t *testing.T, name string) string {
    t.Helper()
    base := filepath.Join(os.TempDir(), "cwtest")
    _ = os.MkdirAll(base, 0o700)
    return filepath.Join(base, fmt.Sprintf("%s-%d", name, time.Now().UnixNano()))
}
```

**Step 3: Update tests to use explicit short socket/db paths**

```go
cfg.Daemon.SocketPath = filepath.Join(shortDir, "cw.sock")
cfg.Daemon.DBPath = filepath.Join(shortDir, "cw.db")
cfg.Daemon.PIDFile = filepath.Join(shortDir, "cw.pid")
```

**Step 4: Re-run tests to verify they pass**

Run: `go test ./internal/cli -run 'Test(StatusCommand_ShowsUnclassifiedToolCount|AuditCodexAgentEndToEnd)' -count=1 -v`
Expected: PASS with no socket bind errors.

**Step 5: Commit**

```bash
git add internal/cli/audit_integration_test.go internal/cli/status_test.go internal/daemon/daemon_test.go
git commit -m "test: use short unix socket paths in integration tests"
```

### Task 1: Commandment Latency Gate (Percentiles + Threshold)

**Files:**
- Modify: `internal/commandments/engine_test.go`
- Test: `internal/commandments/engine_test.go`

**Step 1: Write the failing test (threshold and percentile output format)**

```go
func TestEvalLatencySLO(t *testing.T) {
    // existing setup: 20 rules + representative events
    // add explicit percentile log line used by CI parser
    t.Logf("m3_bench commandment_eval p95=%s p99=%s", p95, p99)
    if p95 >= 2*time.Millisecond {
        t.Fatalf("p95 too high: %s", p95)
    }
    if p99 >= 8*time.Millisecond {
        t.Fatalf("p99 too high: %s", p99)
    }
}
```

**Step 2: Run test to verify it fails (if log/format assertions are missing)**

Run: `go test ./internal/commandments -run TestEvalLatencySLO -count=1 -v`
Expected: FAIL if the expected `m3_bench` output format is not emitted or thresholds are violated.

**Step 3: Write minimal implementation**

```go
sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
p95 := durations[(n*95/100)-1]
p99 := durations[(n*99/100)-1]
t.Logf("m3_bench commandment_eval p95=%s p99=%s", p95, p99)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/commandments -run TestEvalLatencySLO -count=1 -v`
Expected: PASS with one stable log line containing `m3_bench commandment_eval`.

**Step 5: Commit**

```bash
git add internal/commandments/engine_test.go
git commit -m "test: gate commandment eval latency percentiles"
```

### Task 2: Proxy Latency/First-Token Benchmark Harness

**Files:**
- Create: `internal/adapter/proxy/latency_benchmark_test.go`
- Modify: `internal/adapter/proxy/connect_test.go` (reuse helpers if needed)
- Test: `internal/adapter/proxy/latency_benchmark_test.go`

**Step 1: Write the failing benchmark tests (non-stream + stream first-token)**

```go
func TestProxyLatencyGate(t *testing.T) {
    // arrange local mock upstream + proxy
    // run fixed deterministic request set, collect durations
    // compute p95/p99 and assert p95 < 20ms
}

func TestProxyFirstTokenGate(t *testing.T) {
    // stream mock SSE through proxy
    // measure first-token delta (proxy path overhead)
    // assert delta < 50ms
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/proxy -run 'TestProxy(Latency|FirstToken)Gate' -count=1 -v`
Expected: FAIL until percentile collection + assertions are implemented.

**Step 3: Write minimal implementation**

```go
sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
p95 := samples[(len(samples)*95/100)-1]
p99 := samples[(len(samples)*99/100)-1]
t.Logf("m3_bench proxy_roundtrip p95=%s p99=%s", p95, p99)
if p95 >= 20*time.Millisecond { t.Fatalf("p95 too high: %s", p95) }
```

```go
t.Logf("m3_bench proxy_first_token delta=%s", firstTokenDelta)
if firstTokenDelta >= 50*time.Millisecond {
    t.Fatalf("first token delta too high: %s", firstTokenDelta)
}
```

```go
// NOTE: CI regression gate uses reduced deterministic profile.
// Full benchmark profile remains in follow-up benchmark track.
t.Log("m3_bench_profile ci_reduced")
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/adapter/proxy -run 'TestProxy(Latency|FirstToken)Gate' -count=1 -v`
Expected: PASS and stable `m3_bench` log lines for CI parsing.

**Step 5: Commit**

```bash
git add internal/adapter/proxy/latency_benchmark_test.go internal/adapter/proxy/connect_test.go
git commit -m "test: add proxy latency and first-token gates"
```

### Task 3: CI Benchmark Gate Wiring

**Files:**
- Create: `scripts/ci/check_m3_bench.sh`
- Modify: `.github/workflows/ci.yml`
- Modify: `Makefile`
- Test: `scripts/ci/check_m3_bench.sh`

**Step 1: Write the failing CI script test path**

```bash
#!/usr/bin/env bash
set -euo pipefail

go test ./internal/commandments -run TestEvalLatencySLO -count=1 -v | tee /tmp/cmd-bench.log
go test ./internal/adapter/proxy -run 'TestProxy(Latency|FirstToken)Gate' -count=1 -v | tee /tmp/proxy-bench.log

grep -q "m3_bench commandment_eval" /tmp/cmd-bench.log
grep -q "m3_bench proxy_roundtrip" /tmp/proxy-bench.log
grep -q "m3_bench proxy_first_token" /tmp/proxy-bench.log
```

**Step 2: Run script to verify it fails before workflow wiring**

Run: `bash scripts/ci/check_m3_bench.sh`
Expected: FAIL if files/log markers are missing.

**Step 3: Write minimal implementation**

```yaml
# .github/workflows/ci.yml
  benchmark-gate:
    runs-on: ubuntu-latest
    needs: [test]
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: bash scripts/ci/check_m3_bench.sh
```

```make
.PHONY: bench-gate
bench-gate:
	bash scripts/ci/check_m3_bench.sh
```

**Step 4: Run local gate command**

Run: `make bench-gate`
Expected: PASS locally with explicit `m3_bench` markers.

**Step 5: Commit**

```bash
git add scripts/ci/check_m3_bench.sh .github/workflows/ci.yml Makefile
git commit -m "ci: enforce M3 benchmark regression gate"
```

### Task 4: Daemon-Level Proxy E2E Smoke (Allow + Block)

**Files:**
- Create: `internal/daemon/proxy_e2e_test.go`
- Modify: `internal/daemon/daemon_test.go` (shared setup helpers only if needed)
- Test: `internal/daemon/proxy_e2e_test.go`

**Step 1: Write failing E2E tests**

```go
func TestDaemonProxyE2E_AllowPath(t *testing.T) {
    // init temp config + generated CA + mock upstream
    // start daemon
    // send proxied request
    // assert upstream hit >= 1
    // assert audit contains ai_request outcome=success
}

func TestDaemonProxyE2E_BlockPath(t *testing.T) {
    // init temp config + commandment block rule
    // start daemon
    // send proxied request
    // assert upstream hit == 0
    // assert audit contains outcome=blocked and triggered rule metadata
}
```

**Step 2: Run tests to verify failure**

Run: `go test ./internal/daemon -run 'TestDaemonProxyE2E_(AllowPath|BlockPath)' -count=1 -v`
Expected: FAIL until daemon-start + proxy-path assertions are fully wired.

**Step 3: Write minimal implementation**

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go func() { _ = d.Run(ctx) }()

// wait for /health through proxy listener readiness, then execute request
// query audit via store or IPC and assert outcome + triggered metadata
```

```go
// Use a short base directory to avoid unix socket path length errors.
base := filepath.Join(os.TempDir(), "cw")
// or override daemon socket/db paths directly to short absolute paths.
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/daemon -run 'TestDaemonProxyE2E_(AllowPath|BlockPath)' -count=1 -v`
Expected: PASS, including explicit block-path check: upstream hits `0` and audit `outcome=blocked`.

**Step 5: Commit**

```bash
git add internal/daemon/proxy_e2e_test.go internal/daemon/daemon_test.go
git commit -m "test: add daemon-level proxy smoke coverage"
```

### Task 5: Minimal Bubble Tea `watch` UI with Single Reconnect

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/cli/watch_tui.go`
- Modify: `internal/cli/watch.go`
- Create: `internal/cli/watch_tui_test.go`
- Modify: `internal/cli/cli_test.go`
- Test: `internal/cli/watch_tui_test.go`

**Step 1: Write failing TUI model tests first**

```go
func TestWatchModel_UpdatesCountersOnAuditEvent(t *testing.T) {
    // feed audit.event message
    // assert trigger counter and feed rows update
}

func TestWatchModel_ReconnectAttemptOnce(t *testing.T) {
    // simulate disconnect, assert one retry attempt then terminal error state
}

func TestWatchCommand_TextFallbackFlag(t *testing.T) {
    // assert --text keeps legacy plain stream behavior path
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli -run 'TestWatchModel_' -count=1 -v`
Expected: FAIL because model/update logic is not implemented.

**Step 3: Write minimal implementation**

```go
type watchModel struct {
    feed        []string
    queueDepth  int
    queueDropped uint64
    uptime      string
    triggersPerMin int
    reconnectAttempts int
    err error
}
```

```go
func (m watchModel) attemptReconnect() tea.Cmd {
    if m.reconnectAttempts >= 1 {
        return func() tea.Msg { return fatalErrMsg{Err: errors.New("stream disconnected; reconnect failed")} }
    }
    m.reconnectAttempts++
    return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return reconnectMsg{} })
}
```

**Step 4: Run tests and smoke run command**

Run: `go test ./internal/cli -run 'TestWatchModel_' -count=1 -v`
Expected: PASS.

Run: `go test ./internal/cli -run TestWatchCommand_TextFallbackFlag -count=1 -v`
Expected: PASS.

Run: `go test ./internal/cli -run TestRootRegisters -count=1`
Expected: PASS with command registration intact.

**Step 5: Commit**

```bash
git add go.mod go.sum internal/cli/watch.go internal/cli/watch_tui.go internal/cli/watch_tui_test.go
git commit -m "feat: add minimal Bubble Tea watch dashboard"
```

```bash
git add internal/cli/cli_test.go
```

### Task 6: Release Polish (No OTel in M3)

**Files:**
- Modify: `README.md`
- Modify: `docs/plans/2026-02-22-prototype-implementation-design.md`
- Create: `docs/plans/m3-plan-and-tasks.md`
- Test: `.github/workflows/ci.yml` (all jobs green)

**Step 1: Write failing docs checks**

```markdown
- README watch section still describes plain text stream only
- M3 scope still implies OTel in must-ship path
```

**Step 2: Run validation command before edits**

Run: `go test -race -count=1 ./...`
Expected: PASS (baseline before docs-only release alignment).

**Step 3: Write minimal implementation**

```markdown
# README.md
- describe minimal Bubble Tea watch regions
- document one-shot reconnect behavior
- state OTel is deferred from M3
```

```markdown
# docs/plans/m3-plan-and-tasks.md
- list A1/A2/B1/C1 scope
- list explicit non-goals (OTel, advanced filters)
- list sign-off gates
- add explicit deferred SLOs: RSS/event-loss/SQLite throughput follow-up track
- include CI reduced-profile vs full benchmark profile note
```

**Step 4: Run full validation**

Run: `go test -race -count=1 ./...`
Expected: PASS.

Run: `make bench-gate`
Expected: PASS.

**Step 5: Commit**

```bash
git add README.md docs/plans/2026-02-22-prototype-implementation-design.md docs/plans/m3-plan-and-tasks.md
git commit -m "docs: finalize M3 scope and release gates"
```

### Task 7: Final Verification and Merge Readiness

**Files:**
- Modify: none (verification task)
- Test: whole repository + CI workflow

**Step 1: Run targeted M3 tests**

Run: `go test ./internal/commandments -run TestEvalLatencySLO -count=1 -v`
Expected: PASS with `m3_bench` percentile line.

**Step 2: Run proxy and daemon E2E slices**

Run: `go test ./internal/adapter/proxy -run 'TestProxy(Latency|FirstToken)Gate' -count=1 -v`
Expected: PASS.

Run: `go test ./internal/daemon -run 'TestDaemonProxyE2E_(AllowPath|BlockPath)' -count=1 -v`
Expected: PASS.

**Step 3: Run full suite**

Run: `go test -race -count=1 ./...`
Expected: PASS.

**Step 4: Run CI-equivalent bench gate locally**

Run: `make bench-gate`
Expected: PASS.

**Step 5: Commit (if verification-only updates exist)**

```bash
git add -A
git commit -m "chore: verify M3 gates before release" || true
```

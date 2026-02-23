# M1 Plan and Tasks: Commandment Engine + Warn Enforcement

## Context

M0 is complete. M1 adds a YAML-driven commandment engine that evaluates every audit event, applies warn enforcement, redacts sensitive data before persistence, and supports live rule reload.

**Milestone demo:** `crabwise audit --triggered` shows warnings for matched events (for example `.env` access).

## User Stories

1. **As a dev**, I define YAML rules like "warn on `.env` access" and see warnings in `crabwise audit --triggered`
2. **As a dev**, I run `crabwise commandments list` to see active rules and their enforcement level
3. **As a dev**, I run `crabwise commandments test <event-json>` to dry-run a rule against a sample event
4. **As a dev**, I send SIGHUP or `crabwise commandments reload` to reload rules without restarting
5. **As a security-conscious dev**, API keys and credentials in event arguments are redacted before persistence

## Goals

1. Evaluate all active commandments on every event in the logger path (pre-hash).
2. Persist deterministic `commandments_evaluated` and `commandments_triggered` metadata.
3. Apply warning outcomes with deterministic precedence.
4. Redact secrets in `Arguments` before persistence.
5. Support rule reload via SIGHUP and CLI.

## Non-Goals (M1)

- No hard block for log-watcher events (block is reserved for M2 proxy path).
- No `commandments add` mutating command (users edit YAML file, then reload).
- No JSON field extraction matcher for `Arguments` (full string matching only).
- No spend-limit rule (CC logs don't provide cost data; deferred to M2 proxy).

## Locked Decisions

1. **Glob engine:** `github.com/bmatcuk/doublestar/v4`
2. **Match target:** full `Arguments` string
3. **Outcome precedence:** `blocked(3) > failure(2) > warned(1) > success(0)`; only upgrade, never downgrade
4. **Rule ordering:** `priority DESC`, then `name ASC`
5. **Matcher semantics:**
   - case-sensitive by default
   - regex unanchored substring match (`(?i)` allowed for opt-in insensitive)
   - no path normalization for glob
   - list matcher uses exact string equality
   - empty/missing event field → condition fails (no match)
   - non-numeric field for numeric matcher → no match (not an error)
6. **Rule caps:** max 100 rules, max 200 compiled patterns, max 1024 chars/pattern
7. **Redaction caps:** max 50 replacements per field; oversized fields (>1MB) get safe truncation + redacted marker (never persist raw oversized secret content)
8. **Reload observability:** system audit events on load/reload success/failure
9. **Hash determinism:** `commandments_evaluated` serialized in rule evaluation order (priority desc, name asc); `commandments_triggered` serialized in trigger order (same). Stable JSON encoding.
10. **System event exemption:** `commandments_reload_*` and `commandments_load_failed` events are exempt from commandment evaluation/redaction to avoid self-referential triggers

## Architecture Hook

Commandment evaluation and redaction run inside `Logger.processEvent()` before hash computation:

```
processEvent(e):
  1. if not system-event-exempt: Evaluator.Evaluate(e)
  2. if not system-event-exempt: Redactor.Redact(e, ruleTriggered)
  3. assign PrevHash
  4. compute EventHash
```

This keeps governance and redaction tamper-evident in the hash chain.

**Why Logger?** Single-writer guarantee (no concurrency), happens after hostname/userID stamping, before hash. Log watcher events are post-facto — can only warn, never block.

## Data Contract

- Populate `commandments_evaluated` and `commandments_triggered` on every persisted event (except exempt system events).
- Serialize these arrays deterministically (stable rule evaluation order) before hashing.
- For log-watcher adapter, any `block` match is downgraded to `warn`.

## Interfaces

Defined in `internal/audit/` to avoid import cycles. `internal/commandments/` implements them.

```go
// internal/audit/logger.go
type Evaluator interface {
    Evaluate(e *AuditEvent) EvalResult
}
type Redactor interface {
    Redact(e *AuditEvent, ruleTriggered bool)
}
type EvalResult struct {
    Evaluated []string        // rule names, in evaluation order
    Triggered []TriggeredRule // rules that matched, in trigger order
}
type TriggeredRule struct {
    Name        string
    Enforcement string // "warn" | "block"
    Message     string
}
```

## YAML Schema v1

```yaml
version: "1"
commandments:
  - name: no-destructive-commands
    description: Block destructive shell commands
    enforcement: warn   # warn | block (block downgrades to warn on log_watcher)
    priority: 100       # higher = evaluated first
    enabled: true
    match:
      action_type: command_execution
      arguments:
        type: regex
        pattern: "rm\\s+-rf|mkfs|dd\\s+if="
    redact: false
    message: "Destructive command detected"
```

Match conditions AND'd together. Multiple rules evaluated independently (evaluate-all).

## Starter Pack (4 rules)

1. **no-destructive-commands** — regex on `rm -rf`, `mkfs`, `dd if=` in command_execution
2. **protect-credentials** — glob on `**/.env`, `**/*.pem`, `**/*credentials*` in file_access; `redact: true`
3. **approved-models** — list `model not_in [...]` on ai_request
4. **no-push-main** — regex `git push.*(?:main|master)` in command_execution

## New Package: `internal/commandments/`

```
internal/commandments/
  schema.go         — Commandment struct, YAML parsing, LoadFile(), Validate()
  engine.go         — Engine struct implementing audit.Evaluator, Evaluate(), Reload(), Rules()
  matchers.go       — Matcher interface + RegexMatcher, GlobMatcher, NumericMatcher, ListMatcher
  redaction.go      — Redactor implementing audit.Redactor, default patterns, Redact()
  engine_test.go    — eval ordering, precedence, downgrade, caps, latency SLO
  matchers_test.go  — regex, glob, numeric, list semantics + edge cases
  redaction_test.go — token/key patterns, replacement behavior, cap enforcement
  schema_test.go    — valid/invalid YAML, version validation, cap validation
```

## Execution Plan

### Phase 0 — Foundation (sequential)

#### T1: Logger interfaces + precedence
- **Files:** `internal/audit/logger.go`
- Add `Evaluator`, `Redactor`, `EvalResult`, `TriggeredRule`
- Add `SetEvaluator()` and `SetRedactor()`
- Integrate nil-safe evaluate/redact calls in `processEvent`
- Apply outcome precedence upgrade logic (`max(current, new)`) and log-watcher downgrade
- System-event exemption: skip evaluation/redaction for `commandments_reload_*`/`commandments_load_failed` actions

#### T2: Config plumbing
- **Files:** `internal/daemon/config.go`, `configs/default.yaml`
- Add `CommandmentsConfig` with `file` field
- Validate and expand `~` path

### Phase 1 — Parallel Streams

#### Stream A: Engine core

#### T3: Schema + validation
- **Files:** `internal/commandments/schema.go`
- Define `Commandment`, `MatchSpec`, `PatternSpec` structs
- YAML parsing with `version: "1"` check
- Validate: required fields (name, enforcement), valid enums (warn/block), pattern compilation check
- Enforce caps: reject >100 rules, >1024-char patterns at parse time

#### T4: Matcher compilation
- **Files:** `internal/commandments/matchers.go`
- `Matcher` interface: `Match(value string) bool`
- `RegexMatcher` — `regexp.Compile` at load, RE2 guarantees linear time. Case-sensitive, unanchored.
- `GlobMatcher` — `doublestar.Match`. Case-sensitive, no path normalization.
- `NumericMatcher` — `strconv.ParseFloat`, compare with gt/lt/eq/gte/lte. Non-numeric → no match.
- `ListMatcher` — `in`/`not_in` exact string equality, case-sensitive.
- `CompileMatchers()` — compile all patterns at load, fail fast on invalid. Enforce max 200 total compiled patterns.

#### T5: Evaluation engine
- **Files:** `internal/commandments/engine.go`
- `Engine` struct with sorted rules + compiled matchers behind `sync.RWMutex`
- `NewEngine(path string, fallbackYAML []byte) (*Engine, error)` — load from file, fallback to embedded
- `Evaluate(e *audit.AuditEvent) audit.EvalResult` — iterate all enabled rules (priority desc, name asc), AND conditions, collect evaluated/triggered in stable order
- `Reload(path string) error` — parse+compile new rules, atomic swap on success, keep old on failure
- `Rules() []RuleSummary` — for IPC listing

#### T6: Starter pack + embed
- **Files:** `configs/commandments_default.yaml`, `configs/embed.go`
- 4 default rules YAML
- Add `//go:embed commandments_default.yaml` to `configs/embed.go`

#### Stream B: Redaction

#### T7: Redaction pipeline
- **Files:** `internal/commandments/redaction.go`
- `Redactor` struct with compiled always-on patterns
- Default patterns: OpenAI keys (`sk-*`), AWS (`AKIA*`), GitHub PATs (`ghp_*`), Slack tokens (`xox*`), PEM keys, generic `password=/token=/secret=`
- `Redact(e *audit.AuditEvent, ruleTriggered bool)` — always-on + rule-triggered
- Replace matches with `[REDACTED]`, set `e.Redacted = true`
- Max 50 replacements per field
- Oversized (>1MB): safe truncation + redacted marker (never persist raw oversized secret content)

#### Stream C: Fixtures

#### T8: Test fixtures
- **Files:** `testdata/commandments/`
- `valid-basic.yaml`, `valid-all-matchers.yaml`
- `invalid-bad-regex.yaml`, `invalid-missing-name.yaml`, `invalid-bad-enforcement.yaml`, `invalid-bad-version.yaml`
- `invalid-too-many-rules.yaml` (>100 rules)

### Phase 2 — Integration (sequential)

#### T9: Daemon wiring + reload observability
- **Files:** `internal/daemon/daemon.go`
- Initialize engine with embedded fallback, inject evaluator + redactor into logger
- Handle startup load failure non-fatally (emit `commandments_load_failed` system audit event)
- Refactor signal select into loop, add SIGHUP handler
- On reload success: emit `commandments_reload_ok` event (`{"rules_loaded":N}`)
- On reload failure: emit `commandments_reload_failed` event (`{"error":"..."}`)

#### T10: IPC surface
- **Files:** `internal/daemon/daemon.go` (handler registration)
- Add IPC methods:
  - `commandments.list` — returns rule summaries
  - `commandments.test` — accepts event JSON, returns eval result
  - `commandments.reload` — triggers reload, returns success/error

#### T11: CLI commands
- **Files:** `internal/cli/commandments.go`, `internal/cli/root.go`
- `crabwise commandments list` — IPC call, table output (name, enforcement, priority, enabled)
- `crabwise commandments test <event-json>` — IPC call, show evaluated/triggered results
- `crabwise commandments reload` — IPC call to trigger reload
- Register in `root.go`

#### T12: Audit query UX
- **Files:** `internal/cli/audit.go`, `internal/audit/query.go`
- Add `--triggered` flag: filter events where `commandments_triggered` is non-empty
- Add `--outcome` flag: filter by outcome (success/failure/warned/blocked)

#### T13: Init command wiring
- **Files:** `internal/cli/init.go`
- `crabwise init` writes starter commandments to `~/.config/crabwise/commandments.yaml` when absent

### Phase 3 — Validation and Exit Gates

#### T14: Unit tests
- `schema_test.go` — valid/invalid YAML fixtures, version validation, cap enforcement (>100 rules, >1024 pattern)
- `matchers_test.go` — regex (case-sensitive, unanchored), glob (case-sensitive, `**`), numeric (float coercion, non-numeric no-match), list (exact match); empty/missing field → no match
- `engine_test.go` — priority ordering, evaluate-all, outcome precedence matrix (all combos), enforcement downgrade (block→warn on log_watcher), cap enforcement, deterministic serialization order
- `redaction_test.go` — .env content, API keys, PEM, AWS keys, GitHub PATs, Slack tokens; cap enforcement (50 max, >1MB truncation)

#### T15: Integration test
- Daemon start → load defaults → trigger credentials rule → verify `audit --triggered` output with `outcome=warned` and `commandments_triggered` populated

#### T16: Latency SLO harness
- `engine_test.go` `TestEvalLatencySLO` — 10k evaluations (20 rules × varied events), record duration per call, sort, assert `durations[9500] < 2ms` (p95) and `durations[9900] < 8ms` (p99)
- Also add standard `BenchmarkEvaluate` for ns/op baseline

#### T17: Plan/document sync
- Write `docs/plans/m1-plan-and-tasks.md` with full spec
- Ensure `docs/plans/2026-02-22-prototype-implementation-design.md` M1 section remains consistent

## Exit Gates

- [ ] Eval latency: p95 < 2ms, p99 < 8ms (percentile harness, 10k iterations)
- [ ] Rule ordering and conflict behavior covered by tests
- [ ] Outcome precedence matrix tested (all combos, only upgrades)
- [ ] Deterministic serialization for evaluated/triggered metadata verified in hash tests
- [ ] SIGHUP and CLI reload are atomic (bad file preserves old rules)
- [ ] Reload/load failures emit system audit events (in hash chain)
- [ ] System events exempt from commandment evaluation (no self-referential triggers)
- [ ] Block downgrades to warn on log watcher
- [ ] Redaction tests pass for key/token/credential patterns
- [ ] Redaction caps enforced (50 max replacements, >1MB safe truncation)
- [ ] Rule caps validated at load (100 rules, 200 patterns, 1024-char pattern)
- [ ] Matcher semantics match spec (case-sensitive, no normalization, exact list, unanchored regex)
- [ ] `crabwise audit --triggered` shows flagged events
- [ ] `go test -race -count=1 ./...` passes
- [ ] `crabwise commandments list` shows 4 starter rules

## Verification Checklist

1. `go build ./...`
2. `go test -race -count=1 ./...`
3. `crabwise start` logs loaded commandment count
4. `crabwise commandments list` returns 4 defaults
5. `crabwise commandments test '<event-json>'` reports expected matches
6. `kill -HUP <pid>` triggers reload event and keeps service healthy
7. Invalid YAML + reload keeps prior active rule set
8. Secret-containing event persists redacted `Arguments`
9. `crabwise audit --verify-integrity` still passes (redacted content is what's hashed)
10. Benchmark: `go test -run TestEvalLatencySLO ./internal/commandments/`

## Task Tracker

- [ ] T1 Logger interfaces + precedence
- [ ] T2 Config plumbing
- [ ] T3 Schema + validation
- [ ] T4 Matcher compilation
- [ ] T5 Evaluation engine
- [ ] T6 Starter pack + embed
- [ ] T7 Redaction pipeline
- [ ] T8 Fixtures
- [ ] T9 Daemon wiring + reload observability
- [ ] T10 IPC surface
- [ ] T11 CLI commands
- [ ] T12 Audit query UX
- [ ] T13 Init command wiring
- [ ] T14 Unit tests
- [ ] T15 Integration test
- [ ] T16 Latency SLO harness
- [ ] T17 Plan/document sync

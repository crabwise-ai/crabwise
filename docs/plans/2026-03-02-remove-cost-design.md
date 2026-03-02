# Remove Cost Calculation

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove all cost calculation, pricing config, cost display, and cost querying from the codebase. Keep token fields (`input_tokens`, `output_tokens`) — useful for usage monitoring without cost.

**Why:** Cost calculation requires maintaining pricing tables for every model across every provider (OpenAI, Anthropic, Codex, OpenClaw). Model names include version suffixes requiring prefix matching. Not worth the maintenance burden — removes a blocking dependency.

---

## Scope

**Remove:**
- `CostConfig`, `ModelPricing` types and `cost:` YAML config section
- `ComputeCost()`, `CostResult`, `Pricing` struct in proxy adapter
- `CostUSD` field from `AuditEvent`, proxy preflight, otel span data
- `cost_usd` from SQLite INSERT/SELECT (leave column in existing DBs)
- `QueryCostSummary()`, `CostSummaryRow` from audit queries
- `audit.cost` IPC method
- Cost mode in audit TUI (cost table, cost toggle, cost loading)
- `--cost` CLI flag
- Cost display in watch TUI event rendering
- `FormatCost()` from tui helpers
- `cost_usd` commandment field accessor
- `AttrCrabwiseCostUSD` otel attribute
- Cost column from events table display

**Keep:**
- `input_tokens`, `output_tokens` fields everywhere
- Token columns in cost summary SQL (but remove the query itself)
- All non-cost functionality intact

**SQLite migration:** Stop writing `cost_usd`. Remove from CREATE TABLE for new installs. Existing DBs keep the orphaned nullable column harmlessly.

---

### Task 1: Delete proxy cost module, remove Pricing from proxy config

**Files:**
- Delete: `internal/adapter/proxy/cost.go`
- Delete: `internal/adapter/proxy/cost_test.go`
- Modify: `internal/adapter/proxy/provider.go` — remove `Pricing` struct, `Pricing` field from `Config`
- Modify: `internal/adapter/proxy/proxy.go` — remove `ComputeCost` call, `CostUSD` assignments

**Step 1: Run existing proxy tests baseline**

```bash
go test ./internal/adapter/proxy/... -v
```

**Step 2: Delete cost.go and cost_test.go, remove Pricing from provider.go**

In `provider.go`:
- Delete `Pricing` struct (lines 13-16)
- Remove `Pricing map[string]Pricing` from `Config` (line 30)

In `proxy.go`:
- Remove `costResult := ComputeCost(...)` (line 419)
- Remove `preflight.CostUSD = costResult.CostUSD` (line 420)
- Remove `CostUSD: preflight.CostUSD,` from StreamEvent (line 460)

**Step 3: Run tests, fix compile errors**

```bash
go test ./internal/adapter/proxy/... -v
```

**Step 4: Commit**

```bash
git commit -m "remove: proxy cost calculation and Pricing config"
```

---

### Task 2: Remove CostUSD from AuditEvent and canonical serializer

**Files:**
- Modify: `internal/audit/events.go` — remove `CostUSD` field and `appendFloat(buf, e.CostUSD)`

**Step 1: Run audit tests baseline**

```bash
go test ./internal/audit/... -v
```

**Step 2: Remove CostUSD field and serializer line**

In `events.go`:
- Remove `CostUSD float64 \`json:"cost_usd,omitempty"\`` (line 57)
- Remove `buf = appendFloat(buf, e.CostUSD)` (line 101)

**Step 3: Run tests, fix compile errors across codebase**

```bash
go build ./...
go test ./internal/audit/... -v
```

**Step 4: Commit**

```bash
git commit -m "remove: CostUSD from AuditEvent and hash chain serializer"
```

---

### Task 3: Remove cost from SQLite store and audit queries

**Files:**
- Modify: `internal/store/sqlite.go` — remove `cost_usd` from INSERT columns/values, CREATE TABLE
- Modify: `internal/audit/query.go` — remove `cost_usd` from SELECT, remove `CostSummaryRow`, remove `QueryCostSummary()`
- Modify: `internal/audit/query_test.go` — remove `cost_usd` from test schema

**Step 1: Run store and audit tests baseline**

```bash
go test ./internal/store/... ./internal/audit/... -v
```

**Step 2: Edit files**

In `sqlite.go`:
- Remove `cost_usd` from CREATE TABLE column list
- Remove `cost_usd` from INSERT column list and corresponding `?` placeholder
- Remove `e.CostUSD` from bind values

In `query.go`:
- Remove `cost_usd` from SELECT column list in QueryEvents
- Remove `costUSD` scan variable and `e.CostUSD = costUSD.Float64` assignment
- Delete `CostSummaryRow` type
- Delete `QueryCostSummary()` function
- Remove `"cost_usd IS NOT NULL"` condition (was only in cost query)

In `query_test.go`:
- Remove `cost_usd REAL` from test schema

**Step 3: Run tests**

```bash
go test ./internal/store/... ./internal/audit/... -v
```

**Step 4: Commit**

```bash
git commit -m "remove: cost_usd from SQLite schema and audit queries"
```

---

### Task 4: Remove cost from daemon config and IPC

**Files:**
- Modify: `internal/daemon/config.go` — remove `CostConfig`, `ModelPricing`, `Cost` field from Config, pricing defaults, validation
- Modify: `internal/daemon/daemon.go` — remove `audit.cost` IPC handler, pricing wiring to proxy
- Modify: `configs/default.yaml` — remove `cost:` section
- Modify: `internal/daemon/*_e2e_test.go` — remove `Cost:` from test config fixtures

**Step 1: Run daemon tests baseline**

```bash
go test ./internal/daemon/... -v
```

**Step 2: Edit files**

In `config.go`:
- Remove `Cost CostConfig \`yaml:"cost"\`` from Config struct
- Delete `CostConfig` struct
- Delete `ModelPricing` struct
- Remove `cfg.Cost.Pricing = map[string]ModelPricing{...}` from hardcoded defaults
- Remove cost pricing validation (if any)

In `daemon.go`:
- Remove `d.ipcServer.Handle("audit.cost", ...)` handler block
- Remove `pricing := make(map[string]proxy.Pricing, ...)` wiring block
- Remove `Pricing: pricing,` from proxy config construction

In `configs/default.yaml`:
- Delete the `cost:` / `pricing:` block

In e2e test files (`proxy_e2e_test.go`, `redaction_e2e_test.go`, `openclaw_proxy_e2e_test.go`):
- Remove `Cost: CostConfig{...}` from test config construction

**Step 3: Run tests**

```bash
go test ./internal/daemon/... -v
```

**Step 4: Commit**

```bash
git commit -m "remove: cost config, pricing defaults, and audit.cost IPC method"
```

---

### Task 5: Remove cost from audit CLI and TUI

**Files:**
- Modify: `internal/cli/audit.go` — remove `--cost` flag, `cost` variable, cost branching, `showCostSummary()`
- Modify: `internal/cli/audit_tui.go` — remove cost mode (cost table, costRows, totalCost, auditCostLoadedMsg, cost toggle, cost view, auditCostColumns, auditCostToRows, loadAuditCost, cost column from events table)
- Modify: `internal/cli/audit_tui_test.go` — remove cost-related tests and CostUSD from test data

**Step 1: Run CLI tests baseline**

```bash
go test ./internal/cli/... -v
```

**Step 2: Edit audit.go**

- Remove `cost bool` declaration
- Remove `cmd.Flags().BoolVar(&cost, "cost", ...)` flag registration
- Remove `if cost { initialMode = "cost" }` TUI branching
- Remove `if cost { return showCostSummary(...) }` plain branching
- Delete `showCostSummary()` function

**Step 3: Edit audit_tui.go**

- Delete `auditCostLoadedMsg` type
- Remove `costTable`, `costRows`, `totalCost` from `auditTUIModel` struct
- Remove `costTable: ct` from constructor; delete `auditCostColumns()` call and `ct` variable
- Remove `mode` field (always "events" now) — or keep as dead code if simpler
- In `Update()`:
  - Remove `"c"` key toggle between events/cost
  - Remove `case auditCostLoadedMsg:` handler
  - Remove cost table forwarding at bottom
  - Remove `costTable.SetWidth/SetHeight` from WindowSizeMsg
- In `View()`:
  - Remove cost mode banner/table/total rendering block
  - Remove `"c cost view"` from status bar
  - Remove COST column from `auditEventsColumns()`
  - Remove cost display from `auditEventsToRows()`
- Delete `auditCostColumns()`, `auditCostToRows()`, `loadAuditCost()` functions
- In `Init()`: remove cost mode check — always load events

**Step 4: Edit audit_tui_test.go**

- Remove `CostUSD` from test event data
- Remove `TestAuditTUIModel_CostLoaded` test (or equivalent)
- Remove cost-related assertions

**Step 5: Run tests**

```bash
go test ./internal/cli/... -v
```

**Step 6: Commit**

```bash
git commit -m "remove: cost mode from audit CLI and TUI"
```

---

### Task 6: Remove cost from watch TUI, commandments, and otel

**Files:**
- Modify: `internal/cli/watch_tui.go` — remove cost display from event rendering (3 blocks)
- Modify: `internal/commandments/engine.go` — remove `cost_usd` field accessor
- Modify: `internal/commandments/engine_test.go` — remove cost-related test rules and CostUSD from test data
- Modify: `internal/otel/genai.go` — remove `AttrCrabwiseCostUSD`
- Modify: `internal/otel/span.go` — remove `CostUSD` from SpanData, remove cost attribute emission
- Modify: `internal/otel/span_test.go` — remove cost test data and assertions

**Step 1: Run tests baseline**

```bash
go test ./internal/cli/... ./internal/commandments/... ./internal/otel/... -v
```

**Step 2: Edit watch_tui.go**

Remove the 3 cost display blocks (lines ~477-478, ~518-519, ~529-530):
```go
if evt.ActionType == audit.ActionAIRequest && evt.CostUSD > 0 {
    line += " " + argStyle.Render("("+tui.FormatCost(evt.CostUSD)+")")
}
```

**Step 3: Edit commandments**

In `engine.go`: remove `case "cost_usd":` and its return statement
In `engine_test.go`: remove cost_usd rule and CostUSD from test data

**Step 4: Edit otel**

In `genai.go`: remove `AttrCrabwiseCostUSD` constant
In `span.go`: remove `CostUSD float64` from SpanData, remove the cost attribute emission block
In `span_test.go`: remove CostUSD from test data, remove cost attribute assertion

**Step 5: Run tests**

```bash
go test ./internal/cli/... ./internal/commandments/... ./internal/otel/... -v
```

**Step 6: Commit**

```bash
git commit -m "remove: cost from watch TUI, commandments engine, and otel spans"
```

---

### Task 7: Remove FormatCost and update docs

**Files:**
- Modify: `internal/tui/format.go` — delete `FormatCost()` function
- Modify: `internal/tui/format_test.go` — delete `TestFormatCost` test
- Modify: `README.md` — remove `crabwise.cost_usd` otel reference, any cost CLI examples
- Modify: `internal/cli/watch_tui_test.go` — remove CostUSD from test data if present

**Step 1: Edit format.go and format_test.go**

Delete `FormatCost` function and `TestFormatCost` test.

**Step 2: Grep for any remaining cost references**

```bash
rg -n 'cost|Cost|COST|pricing|Pricing' --type go --type yaml | grep -v '_test.go' | grep -vi 'no.cost'
```

Fix any stragglers.

**Step 3: Update README**

Remove references to `crabwise.cost_usd` attribute and `crabwise audit --cost`.

**Step 4: Run full test suite**

```bash
go test ./...
golangci-lint run ./...
```

**Step 5: Commit**

```bash
git commit -m "remove: FormatCost helper and cost references from docs"
```

---

### Task 8: Fix arrow navigation in audit events TUI

**Files:**
- Modify: `internal/cli/audit_tui.go` — verify up/down arrow forwarding works after cost removal cleanup

After cost removal, the table forwarding code simplifies to just the events table. Verify:
- Up/down arrows navigate rows in the events table
- The events table is focused on init
- Table forwarding runs for key messages that don't match the inner switch

If navigation is broken, fix by ensuring table.Update receives KeyMsg for up/down.

**Step 1: Run audit TUI tests**

```bash
go test ./internal/cli -run TestAudit -v
```

**Step 2: Manual verification**

```bash
go run ./cmd/crabwise audit
```

Verify arrow keys scroll through event rows.

**Step 3: Commit if changes needed**

```bash
git commit -m "fix: audit TUI arrow key navigation"
```

---

### Task 9: Final verification

```bash
go test ./...
golangci-lint run ./...
go build -o /dev/null ./cmd/crabwise
git log --oneline -n 10
```

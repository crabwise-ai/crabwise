# OpenClaw Governance Adapter Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add first-class OpenClaw support to Crabwise for provider-call governance by observing OpenClaw Gateway state and enriching proxy enforcement events with OpenClaw session identity.

**Architecture:** Keep blocking in the existing forward proxy. Add a read-only OpenClaw Gateway adapter plus an in-memory correlation store keyed by `runId` and `sessionKey`. Use that correlation store from the proxy to stamp enforcing audit events with OpenClaw identity when the match is confident, while degrading safely when the Gateway is absent or ambiguous.

**Tech Stack:** Go, Cobra, Bubble Tea, SQLite, fsnotify, HTTP proxy, JSON-RPC IPC, `github.com/gorilla/websocket`

---

### Task 1: Add OpenClaw config schema and defaults

**Files:**
- Modify: `internal/daemon/config.go`
- Modify: `configs/default.yaml`
- Test: `internal/daemon/config_test.go`

**Step 1: Write the failing tests**

Add table-driven tests for:

- default config loads `adapters.openclaw.enabled=false`
- `gateway_url` must be non-empty when adapter enabled
- `session_refresh_interval` and `correlation_window` must be positive when adapter enabled

Suggested test shape:

```go
func TestLoadConfig_OpenClawDefaults(t *testing.T) {
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.False(t, cfg.Adapters.OpenClaw.Enabled)
	require.Equal(t, "ws://127.0.0.1:18789", cfg.Adapters.OpenClaw.GatewayURL)
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/daemon -run 'TestLoadConfig_OpenClawDefaults|TestLoadConfig_OpenClawValidation' -v
```

Expected: FAIL because `OpenClawConfig` does not exist yet.

**Step 3: Write minimal implementation**

Add:

- `OpenClawConfig` to `AdaptersConfig`
- fields:
  - `Enabled bool`
  - `GatewayURL string`
  - `APITokenEnv string`
  - `SessionRefreshInterval Duration`
  - `CorrelationWindow Duration`
- defaults in hardcoded fallback and `configs/default.yaml`
- validation in `(*Config).validate()`

Use these initial defaults:

```yaml
adapters:
  openclaw:
    enabled: false
    gateway_url: ws://127.0.0.1:18789
    api_token_env: OPENCLAW_API_TOKEN
    session_refresh_interval: 30s
    correlation_window: 3s
```

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/daemon -run 'TestLoadConfig_OpenClawDefaults|TestLoadConfig_OpenClawValidation' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/config.go internal/daemon/config_test.go configs/default.yaml
git commit -m "feat: add openclaw adapter config"
```

### Task 2: Define OpenClaw Gateway protocol types and config translation

**Files:**
- Create: `internal/adapter/openclaw/protocol.go`
- Create: `internal/adapter/openclaw/config.go`
- Test: `internal/adapter/openclaw/protocol_test.go`

**Step 1: Write the failing tests**

Add tests for:

- decoding `event` frames for `chat`, `agent`, `exec.started`, `exec.completed`
- decoding `hello-ok`
- parsing session keys into stable `agentID/platform/recipient`

Suggested test cases:

```go
func TestParseSessionKey(t *testing.T) {
	got := ParseSessionKey("agent:main:discord:channel:123")
	require.Equal(t, "main", got.AgentID)
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/adapter/openclaw -run 'TestParseSessionKey|TestDecodeGatewayFrame' -v
```

Expected: FAIL because package and types do not exist.

**Step 3: Write minimal implementation**

Port the minimum protocol model from `/home/del/Github/crabwise-genai/src/gateway/protocol.ts`:

- request/response/event frame envelopes
- `ChatEvent`
- `AgentEvent`
- `ExecStartedEvent`
- `ExecCompletedEvent`
- `SessionInfo`
- `ParseSessionKey`

Add `Config` translation helpers that map daemon config into adapter runtime config.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/adapter/openclaw -run 'TestParseSessionKey|TestDecodeGatewayFrame' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/adapter/openclaw/protocol.go internal/adapter/openclaw/config.go internal/adapter/openclaw/protocol_test.go
git commit -m "feat: add openclaw gateway protocol model"
```

### Task 3: Implement Gateway client and session cache

**Files:**
- Create: `internal/adapter/openclaw/client.go`
- Create: `internal/adapter/openclaw/session_cache.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Test: `internal/adapter/openclaw/client_test.go`
- Test: `internal/adapter/openclaw/session_cache_test.go`

**Step 1: Write the failing tests**

Add tests covering:

- connection handshake against a fake WebSocket server
- reconnect-safe event callback handling
- `sessions.list` refresh updates cache
- missing API token only affects authenticated connect, not config parsing

Suggested test shape:

```go
func TestSessionCacheRefresh(t *testing.T) {
	client := &fakeGatewayClient{sessions: []SessionInfo{{Key: "agent:main:x", AgentID: "main"}}}
	cache := NewSessionCache(client, time.Minute)
	require.NoError(t, cache.Refresh(context.Background()))
	got, ok := cache.Get("agent:main:x")
	require.True(t, ok)
	require.Equal(t, "main", got.AgentID)
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/adapter/openclaw -run 'TestSessionCacheRefresh|TestGatewayClientConnect' -v
```

Expected: FAIL because the client and cache do not exist.

**Step 3: Write minimal implementation**

First add the dependency explicitly:

```bash
go get github.com/gorilla/websocket
go mod tidy
```

Implement:

- a small WebSocket client using `github.com/gorilla/websocket`
- request/response correlation for `sessions.list`
- event subscription callback registration
- `SessionCache` keyed by `sessionKey`

Keep the client intentionally small:

- one live connection
- one reader loop
- no persistence
- reconnect can be deferred until later unless required by tests

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/adapter/openclaw -run 'TestSessionCacheRefresh|TestGatewayClientConnect' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add go.mod go.sum internal/adapter/openclaw/client.go internal/adapter/openclaw/session_cache.go internal/adapter/openclaw/client_test.go internal/adapter/openclaw/session_cache_test.go
git commit -m "feat: add openclaw gateway client and session cache"
```

### Task 4: Build the OpenClaw correlation store

**Files:**
- Create: `internal/openclawstate/store.go`
- Create: `internal/openclawstate/types.go`
- Test: `internal/openclawstate/store_test.go`

**Step 1: Write the failing tests**

Add tests for:

- storing `runId -> sessionKey`
- storing `sessionKey -> metadata`
- matching a recent provider request using time window plus model/provider agreement
- refusing ambiguous matches

Suggested test shape:

```go
func TestMatchRequest_AmbiguousReturnsFalse(t *testing.T) {
	store := New(3 * time.Second)
	// insert two candidate sessions with equal score
	_, ok := store.MatchProxyRequest(time.Now(), "anthropic", "claude-sonnet")
	require.False(t, ok)
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/openclawstate -run 'TestMatchRequest|TestRecordSession' -v
```

Expected: FAIL because the package does not exist.

**Step 3: Write minimal implementation**

Define:

- `SessionMeta`
- `RecentChat`
- `MatchResult`
- thread-safe store with bounded in-memory maps

Required public methods:

- `RecordSession(meta SessionMeta)`
- `RecordChat(runID, sessionKey, provider, model string, ts time.Time)`
- `RecordRun(runID, sessionKey string)`
- `MatchProxyRequest(ts time.Time, provider, model string) (MatchResult, bool)`
- `Snapshot() map[string]any`

Start with simple scoring:

- exact provider match
- exact model match if present
- smallest absolute time delta inside `correlation_window`
- reject ties

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/openclawstate -run 'TestMatchRequest|TestRecordSession' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/openclawstate/types.go internal/openclawstate/store.go internal/openclawstate/store_test.go
git commit -m "feat: add openclaw proxy correlation state"
```

### Task 5: Implement the read-only OpenClaw adapter

**Files:**
- Create: `internal/adapter/openclaw/openclaw.go`
- Create: `internal/adapter/openclaw/mapper.go`
- Test: `internal/adapter/openclaw/openclaw_test.go`
- Modify: `internal/adapter/adapter.go`

**Step 1: Write the failing tests**

Add tests proving:

- adapter starts against a fake Gateway
- adapter emits visibility `AuditEvent`s for `chat` and `agent` activity
- adapter updates the correlation store from live events and session refresh
- `CanEnforce()` returns `false`

Suggested event assertion:

```go
require.Equal(t, "openclaw", evt.AgentID)
require.Equal(t, audit.ActionAIRequest, evt.ActionType)
require.Equal(t, "openclaw-gateway", evt.AdapterID)
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/adapter/openclaw -run 'TestAdapterStart|TestAdapterCanEnforce' -v
```

Expected: FAIL because the adapter does not exist.

**Step 3: Write minimal implementation**

Implement:

- `type Adapter struct`
- `Start(ctx context.Context, events chan<- *audit.AuditEvent) error`
- `Stop() error`
- `CanEnforce() bool`

Behavior:

- connect client
- start periodic `sessions.list` refresh
- convert Gateway events into visibility `AuditEvent`s
- push session and run metadata into `internal/openclawstate`

Do not overbuild:

- no durable offsets
- no replay
- no enforcement path

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/adapter/openclaw -run 'TestAdapterStart|TestAdapterCanEnforce' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/adapter/adapter.go internal/adapter/openclaw/openclaw.go internal/adapter/openclaw/mapper.go internal/adapter/openclaw/openclaw_test.go
git commit -m "feat: add openclaw gateway observer adapter"
```

### Task 6: Wire the adapter into the daemon and registry

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/discovery/registry.go`
- Modify: `internal/discovery/scanner.go`
- Test: `internal/daemon/daemon_test.go`
- Test: `internal/discovery/registry_test.go`

**Step 1: Write the failing tests**

Add tests for:

- daemon starts the OpenClaw adapter when enabled
- `status` includes OpenClaw observer snapshot fields
- registry includes OpenClaw session agents without relying on filesystem scans
- registry can hold scanned agents and Gateway-fed OpenClaw session agents at the same time without the next `Update()` call deleting the OpenClaw entries
- PID-less OpenClaw agents remain stable across refreshes and use session recency, not PID, to drive status

Suggested assertions:

```go
require.Equal(t, float64(1), status["openclaw_connected"])
require.GreaterOrEqual(t, status["openclaw_session_cache_size"], float64(1))
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/daemon -run 'TestDaemonStatusIncludesOpenClaw|TestDaemonStartsOpenClawAdapter' -v
```

Expected: FAIL because the daemon does not wire the adapter or expose status fields.

**Step 3: Write minimal implementation**

Modify daemon lifecycle to:

- instantiate `internal/openclawstate`
- start `internal/adapter/openclaw` when configured
- expose `openclaw_connected`, `openclaw_session_cache_size`, `openclaw_correlation_matches`, and `openclaw_correlation_ambiguous` in `status`

Modify discovery/registry carefully. The current `Registry.Update()` flow is scan-replace semantics and is not sufficient for PID-less Gateway sessions.

Implement a source-aware registry method, for example:

```go
func (r *Registry) ReplaceSource(source string, agents []AgentInfo)
```

Expected usage:

- discovery loop continues to call `ReplaceSource("scanner", scannedAgents)`
- OpenClaw adapter calls `ReplaceSource("openclaw-gateway", openclawAgents)` after each session refresh

Behavior requirements:

- entries from one source do not delete entries from another source
- `AgentInfo.ID` for OpenClaw must be stable, for example `openclaw/<sessionKey>`
- `PID` is allowed to remain `0` for OpenClaw session agents
- `Status` for OpenClaw agents comes from Gateway `lastActivityAt` recency, not `/proc`

Phase 1 discovery scope:

- primary source is Gateway `sessions.list`
- optional process-signature presence detection can be added in `scanner.go` only if it stays clearly separate from session discovery
- `~/.openclaw` filesystem fallback is explicitly deferred and should not be implemented in phase 1

Do not overload the existing scanner loop with Gateway semantics. Keep the OpenClaw adapter responsible for producing session-shaped agent records.

Hot reload stance for phase 1:

- do not extend `reloadRuntime()` to restart or reconfigure the OpenClaw adapter
- document that changes to `adapters.openclaw.*` require a daemon restart
- add a focused test proving existing SIGHUP behavior is unchanged for commandments, tool registry, and proxy mappings

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/daemon -run 'TestDaemonStatusIncludesOpenClaw|TestDaemonStartsOpenClawAdapter' -v
go test ./internal/discovery -run 'TestRegistryReplaceSource_' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/daemon_test.go internal/discovery/registry.go internal/discovery/registry_test.go internal/discovery/scanner.go
git commit -m "feat: wire openclaw adapter into daemon"
```

### Task 7: Enrich proxy events with OpenClaw correlation

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/adapter/proxy/proxy.go`
- Modify: `internal/adapter/proxy/provider.go`
- Create: `internal/adapter/proxy/openclaw_enrichment_test.go`

**Step 1: Write the failing tests**

Add tests proving:

- matched proxy requests are stamped with `agent_id=openclaw`
- `session_id` is populated from `sessionKey`
- ambiguous matches leave `session_id` empty
- blocked requests still never reach upstream

Suggested test shape:

```go
func TestBuildAuditEvent_UsesOpenClawMatch(t *testing.T) {
	store := fakeAttributor{result: MatchResult{AgentID: "openclaw", SessionKey: "agent:main:x"}}
	p := newProxyWithAttributor(store)
	evt := p.buildAuditEvent("req-1", now, "anthropic", req, "/v1/messages")
	require.Equal(t, "openclaw", evt.AgentID)
	require.Equal(t, "agent:main:x", evt.SessionID)
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/adapter/proxy -run 'TestBuildAuditEvent_UsesOpenClawMatch|TestProxyBlockWithOpenClawAttribution' -v
```

Expected: FAIL because proxy enrichment hooks do not exist.

**Step 3: Write minimal implementation**

Define the attribution contract in `internal/adapter/proxy/provider.go`, because that file already holds shared runtime interfaces and transport contracts:

```go
type RequestAttributor interface {
	MatchProxyRequest(ts time.Time, provider, model string) (openclawstate.MatchResult, bool)
}
```

Injection path:

- add `attributor RequestAttributor` to `Proxy` in `internal/adapter/proxy/proxy.go`
- add `func (p *Proxy) SetRequestAttributor(a RequestAttributor)` in `internal/adapter/proxy/proxy.go`
- in `internal/daemon/daemon.go`, after creating the OpenClaw state and proxy, call `d.proxy.SetRequestAttributor(d.openclawState)`

Behavior:

- call the attributor from `buildAuditEvent`
- set `AgentID`, `SessionID`, `ParentSessionID`, and OpenClaw metadata in `Arguments`
- keep `AdapterID` and `AdapterType` as proxy values

Do not put the attributor onto daemon config or proxy config. It is a runtime dependency, not user configuration.

Do not change commandment evaluation order.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/adapter/proxy -run 'TestBuildAuditEvent_UsesOpenClawMatch|TestProxyBlockWithOpenClawAttribution' -v
go test ./internal/daemon -run 'TestDaemonInjectsOpenClawAttributor' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/adapter/proxy/proxy.go internal/adapter/proxy/provider.go internal/adapter/proxy/openclaw_enrichment_test.go
git commit -m "feat: enrich proxy events with openclaw identity"
```

### Task 8: Surface OpenClaw in CLI and TUI

**Files:**
- Modify: `internal/cli/agents.go`
- Modify: `internal/cli/status.go`
- Modify: `internal/cli/watch.go`
- Modify: `internal/cli/watch_tui.go`
- Modify: `internal/cli/audit.go`
- Modify: `internal/cli/audit_tui.go`
- Modify: `internal/cli/status_tui.go`
- Modify: `internal/cli/agents_tui.go`
- Test: `internal/cli/status_test.go`
- Test: `internal/cli/agents_tui_test.go`
- Test: `internal/cli/watch_tui_test.go`
- Test: `internal/cli/watch_text_test.go`
- Test: `internal/cli/audit_tui_test.go`

**Step 1: Write the failing tests**

Add tests for:

- plain `status` output includes OpenClaw observer fields when present
- agents views do not regress when OpenClaw sessions have no PID
- TUI renders OpenClaw sessions without truncating session identity into useless output
- `watch --text` shows OpenClaw observer events and OpenClaw-attributed proxy blocks with readable agent/session output
- watch TUI filtering still works when feed rows include OpenClaw observer events
- existing `audit --agent openclaw --session <sessionKey>` flow is covered by tests, even if no new flags are needed
- audit TUI event rows remain readable when OpenClaw session IDs are present

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/cli -run 'TestStatusCommand_ShowsOpenClaw|TestAgentsTUI_ShowsOpenClaw|TestWatchText_ShowsOpenClawEvents|TestAuditTUI_OpenClawSessionRows' -v
```

Expected: FAIL because the CLI surfaces do not consistently render or verify OpenClaw-specific data yet.

**Step 3: Write minimal implementation**

Update:

- plain status output
- TUI status summary
- agent list renderers
- watch text formatter
- watch TUI feed rendering and filtering as needed
- audit row formatting and tests for existing `--agent` and `--session` behavior with OpenClaw identities

Important: `internal/cli/audit.go` already supports generic `--agent` and `--session` filtering. Do not add redundant flags. The work here is to verify that existing filters behave correctly for OpenClaw IDs and that the visible output remains useful.

Keep output additive. Do not redesign the TUI.

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/cli -run 'TestStatusCommand_ShowsOpenClaw|TestAgentsTUI_ShowsOpenClaw|TestWatchText_ShowsOpenClawEvents|TestAuditTUI_OpenClawSessionRows' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/agents.go internal/cli/status.go internal/cli/watch.go internal/cli/watch_tui.go internal/cli/audit.go internal/cli/audit_tui.go internal/cli/status_tui.go internal/cli/agents_tui.go internal/cli/status_test.go internal/cli/agents_tui_test.go internal/cli/watch_tui_test.go internal/cli/watch_text_test.go internal/cli/audit_tui_test.go
git commit -m "feat: surface openclaw events across cli views"
```

### Task 9: Add end-to-end integration coverage

**Files:**
- Create: `internal/daemon/openclaw_proxy_e2e_test.go`
- Modify: `internal/adapter/proxy/connect_test.go`
- Modify: `internal/adapter/proxy/router_test.go`

**Step 1: Write the failing tests**

Add end-to-end tests for:

- fake OpenClaw Gateway session plus proxied provider request yields attributed event
- blocked attributed request never reaches upstream
- Gateway disconnected still blocks, but emits unattributed proxy event

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/daemon -run 'TestOpenClawProxyE2E_' -v
```

Expected: FAIL because the OpenClaw-aware test harness and attribution wiring do not exist.

**Step 3: Write minimal implementation**

Use fake components only:

- fake WebSocket Gateway
- fake upstream provider
- real Crabwise proxy
- real commandment evaluator

Assert:

- upstream hit count
- audit event `AgentID`
- audit event `SessionID`
- audit event `Outcome`

**Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/daemon -run 'TestOpenClawProxyE2E_' -v
go test ./internal/adapter/proxy -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/daemon/openclaw_proxy_e2e_test.go internal/adapter/proxy/connect_test.go internal/adapter/proxy/router_test.go
git commit -m "test: add openclaw proxy attribution e2e coverage"
```

### Task 10: Document the user path

**Files:**
- Modify: `README.md`
- Modify: `docs/plans/proxy_enforcement-howto.md`

**Step 1: Write the failing docs check**

There is no automated docs check here, so use a grep-based verification target:

```bash
rg -n "openclaw|OPENCLAW_API_TOKEN|gateway_url" README.md docs/plans/proxy_enforcement-howto.md
```

Expected before edits: missing or incomplete OpenClaw guidance.

**Step 2: Write minimal documentation**

Document:

- enabling the OpenClaw adapter
- running OpenClaw through `crabwise wrap`
- Gateway optionality for attribution
- phase 1 restart requirement for `adapters.openclaw.*` config changes
- what phase 1 does and does not govern

Keep the docs honest: provider-call governance only.

**Step 3: Verify documentation**

Run:

```bash
rg -n "openclaw|OPENCLAW_API_TOKEN|gateway_url" README.md docs/plans/proxy_enforcement-howto.md
```

Expected: matches for setup, config, and limitations.

**Step 4: Commit**

```bash
git add README.md docs/plans/proxy_enforcement-howto.md
git commit -m "docs: add openclaw governance setup guide"
```

### Task 11: Final verification pass

**Files:**
- No code changes required unless failures appear

**Step 1: Run focused package tests**

Run:

```bash
go test ./internal/adapter/openclaw ./internal/openclawstate ./internal/adapter/proxy ./internal/daemon ./internal/cli -v
```

Expected: PASS

**Step 2: Run the broader regression suite**

Run:

```bash
go test ./... 
```

Expected: PASS

**Step 3: Inspect git diff**

Run:

```bash
git status --short
git log --oneline -n 5
```

Expected: only intended OpenClaw files remain modified; commits are granular and readable.

**Step 4: Commit any final fixups**

```bash
git add -A
git commit -m "chore: finalize openclaw governance adapter rollout"
```

Only do this if Task 11 required cleanup changes.

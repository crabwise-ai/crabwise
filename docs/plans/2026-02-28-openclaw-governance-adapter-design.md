# OpenClaw Governance Adapter Design

Date: 2026-02-28
Status: approved for planning

## Summary

This design adds first-class OpenClaw support to Crabwise for provider-call governance. Enforcement remains in Crabwise's existing forward proxy. A new read-only OpenClaw adapter connects to the OpenClaw Gateway to observe live session and run state, then enriches proxy audit events with authoritative OpenClaw identity.

This phase intentionally targets the same governance boundary Crabwise already supports for Codex and Claude through the proxy:

- block or warn provider requests before they reach upstream APIs
- attribute those requests to the correct OpenClaw session when correlation is confident
- degrade safely when Gateway observation is unavailable

This phase does not attempt to govern local tool execution or Gateway-side task execution after the model response has already reached OpenClaw.

## Research Basis

The design is based on the current Crabwise codebase plus current OpenClaw Gateway details observed in the user's prototype bridge at `/home/del/Github/crabwise-genai`.

Relevant OpenClaw Gateway signals from the prototype:

- WebSocket event stream with `chat`, `agent`, `exec.started`, and `exec.completed`
- request/response protocol including `sessions.list`
- session metadata keyed by `sessionKey`
- run correlation keyed by `runId`
- session metadata including `agentId`, `model`, `thinkingLevel`, and `spawnedBy`

The original request asked for Exa MCP research. Exa was added to the global Codex config, but it was not mounted into this session's MCP runtime. Public OpenClaw docs and the local prototype repository were used as fallback sources.

## Goals

- Treat OpenClaw as a first-class monitored agent in Crabwise
- Preserve the existing proxy as the only enforcement point
- Attribute blocked and allowed provider calls to OpenClaw sessions when correlation is strong
- Keep Crabwise functional when OpenClaw Gateway is missing, disconnected, or drifting
- Avoid brittle filesystem-only heuristics as the primary identity source

## Non-Goals

- Blocking OpenClaw local tool execution in this phase
- Modifying OpenClaw internals or requiring an OpenClaw plugin
- Migrating the Crabwise audit schema for OpenClaw-specific columns in phase 1
- Building OpenClaw-specific CLI commands beyond config and visibility integration

## Recommended Approach

Use a Gateway-aware observer plus proxy enrichment.

Crabwise will add a new OpenClaw adapter that connects to the OpenClaw Gateway in read-only mode. That adapter maintains an in-memory cache of active sessions and runs. The existing proxy remains the enforcement path and consults the OpenClaw correlation state to enrich audit events with OpenClaw identity.

This split keeps governance generic and infrastructure-level while using the Gateway only for the information it is best positioned to provide: authoritative session and run metadata.

## Alternatives Considered

### 1. Proxy-only support

Treat OpenClaw as a generic wrapped client and rely only on proxy traffic.

Pros:

- fastest to ship
- no Gateway dependency

Cons:

- weak attribution
- poor `agents` UX
- blocked requests would appear as generic proxy events instead of OpenClaw session events

### 2. Filesystem-only observer

Use `~/.openclaw` files and logs as the sole source of OpenClaw identity.

Pros:

- no live Gateway dependency
- familiar shape relative to the current log watcher

Cons:

- lower confidence correlation
- more drift-prone
- weaker live session visibility than Gateway events

### 3. Gateway-aware observer plus proxy enrichment

Use the Gateway for observation and session metadata while keeping enforcement in the proxy.

Pros:

- strongest attribution without moving enforcement
- best fit with current Crabwise architecture
- avoids guessing from files when authoritative live session data exists

Cons:

- requires maintaining a Gateway protocol client
- Gateway drift becomes an observation risk

This is the recommended approach.

## Architecture

### Components

#### 1. OpenClaw adapter

Add a new package, expected shape:

- `internal/adapter/openclaw`

Responsibilities:

- connect to the OpenClaw Gateway WebSocket
- authenticate if needed using configured token env
- subscribe to live events
- periodically call `sessions.list`
- maintain an in-memory cache keyed by `sessionKey`
- emit non-enforcing audit events for OpenClaw-native activity

This adapter returns `CanEnforce() == false`.

#### 2. OpenClaw correlation state

Add a small internal state module, for example:

- `internal/openclawstate`

Responsibilities:

- track recent `runId -> sessionKey`
- track `sessionKey -> agentId/model/thinkingLevel/spawnedBy`
- retain a short rolling window of recent chat activity for correlation
- expose read-only lookup methods to the proxy

This state is not persisted in phase 1. It is ephemeral runtime correlation state.

#### 3. Existing proxy

The proxy remains the only component that can block.

Changes:

- consult OpenClaw correlation state before finalizing audit events
- replace generic `agent_id=proxy` with OpenClaw identity when correlation is confident
- keep `adapter_type=proxy` so enforcement provenance remains explicit

## Data Flow

### Observer stream

1. Crabwise starts the OpenClaw adapter.
2. The adapter connects to the Gateway URL.
3. It receives `chat`, `agent`, `exec.started`, and `exec.completed` events.
4. It periodically refreshes `sessions.list`.
5. It updates the correlation cache and emits visibility events.

### Enforcement stream

1. User launches OpenClaw through `crabwise wrap`.
2. OpenClaw provider traffic hits the Crabwise forward proxy.
3. The proxy normalizes the request and evaluates commandments.
4. Before emitting the audit event, the proxy asks the OpenClaw correlation layer whether this request can be attributed to an active OpenClaw session.
5. If matched confidently, the proxy emits an enforcing audit event with OpenClaw identity attached.
6. If blocked, the request never reaches upstream.

### Correlation strategy

Use the following order:

1. explicit OpenClaw-identifying header or user-agent if present
2. recent Gateway chat activity within a short time window plus provider/model agreement
3. fallback to generic OpenClaw attribution only if a wrapped OpenClaw process is known but session-level correlation is ambiguous

If correlation is ambiguous, Crabwise must still enforce but must not invent a session ID.

## Audit Event Model

Phase 1 should avoid a schema migration and store OpenClaw-specific fields in `Arguments`.

When correlation is strong, proxy events should be emitted roughly as:

- `agent_id = "openclaw"`
- `session_id = <sessionKey>`
- `parent_session_id = <spawnedBy>` when available
- `adapter_id = "proxy"`
- `adapter_type = "proxy"`

OpenClaw-specific metadata should be appended into `Arguments`, for example:

- `openclaw.run_id`
- `openclaw.session_key`
- `openclaw.agent_id`
- `openclaw.thinking_level`
- `openclaw.correlation_confidence`

The OpenClaw observer itself can emit non-enforcing events with:

- `agent_id = "openclaw"`
- `adapter_id = "openclaw-gateway"`
- `adapter_type = "gateway_observer"`

These events are for visibility only and should never be mistaken for enforcement events.

## App Surface

### Config

Add:

```yaml
adapters:
  openclaw:
    enabled: true
    gateway_url: ws://127.0.0.1:18789
    api_token_env: OPENCLAW_API_TOKEN
    session_refresh_interval: 30s
    correlation_window: 3s
```

The exact durations can be tuned during implementation.

### Discovery

Crabwise should surface OpenClaw in `crabwise agents` using Gateway data first.

Discovery priority:

1. Gateway `sessions.list`
2. process signatures if useful for presence detection
3. optional `~/.openclaw` fallback if Gateway is unavailable

Session identity should be based on `sessionKey`, not filesystem naming heuristics.

### CLI and TUI

Changes:

- `crabwise agents` shows OpenClaw sessions as first-class agents
- `crabwise watch` shows OpenClaw observer events and OpenClaw-attributed proxy blocks
- `crabwise audit` can filter by `agent=openclaw` and `session=<sessionKey>`
- `crabwise status` reports OpenClaw observer health, cache size, and correlation counts

### Operational UX

Primary user flow remains:

```bash
crabwise start
crabwise wrap -- openclaw ...
```

Gateway connectivity improves attribution but is not required for proxy enforcement.

## Failure Modes

### Gateway unavailable

- proxy enforcement continues normally
- attribution degrades
- Crabwise emits a system event such as `openclaw_gateway_disconnected`

### Gateway protocol drift

- parser handles unknown events non-fatally
- `sessions.list` failure does not break enforcement
- Crabwise records observer health degradation and unknown event counts

### Correlation ambiguity

- enforce anyway
- do not invent `session_id`
- add `openclaw.correlation_confidence=low` or equivalent marker

### OpenClaw bypasses proxy

- Gateway visibility may still show activity
- proxy events will be absent
- Crabwise should surface this as a routing/config issue, not as an adapter failure

## Testing Strategy

### Unit tests

- Gateway frame decoding
- session key parsing and normalization
- session cache update logic
- correlation scoring and tie-breaking
- degraded-mode behavior when Gateway is disconnected

### Integration tests

- fake Gateway server emitting `chat` and `agent` events
- fake `sessions.list` response
- proxy request correctly enriched with OpenClaw session identity
- blocked request never reaches upstream while retaining OpenClaw attribution

### Regression tests

- Gateway absent, proxy still blocks
- ambiguous attribution leaves `session_id` blank
- observer emits visibility events without changing proxy enforcement semantics

## Rollout Plan

Phase 1:

- OpenClaw Gateway adapter
- in-memory session and run cache
- proxy enrichment hook
- CLI and TUI identity support
- tests for correlation and degraded operation

Defer:

- schema migration for first-class OpenClaw fields
- OpenClaw tool-execution governance
- OpenClaw-specific commands beyond baseline visibility

## Open Questions For Planning

- Which explicit headers or user-agent markers does OpenClaw actually send on provider requests, if any
- Whether Gateway auth is always required in local setups or only in some configurations
- Whether `sessionKey` should map directly to `session_id` or if a normalized derivative is needed for UX consistency
- Whether OpenClaw observer events should be persisted at full fidelity or reduced to lifecycle-only in phase 1

## Recommendation

Implement OpenClaw support as a Gateway-aware read-only adapter plus proxy correlation layer. Keep all enforcement in the existing proxy. This yields full governance over provider calls with the highest attribution quality available without requiring OpenClaw modifications.

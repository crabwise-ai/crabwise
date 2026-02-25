# M2 Plan and Tasks: Proxy + Block Enforcement

## Context

M1.5 is complete and stable. M2 adds a provider-agnostic local proxy framework that enforces commandments before upstream requests, captures GenAI telemetry, tracks spend, and hardens payload handling for high-volume streams. OpenAI-compatible transport is implemented first as the reference adapter, while Claude Code remains a first-class supported path via the existing log watcher.

**Milestone demo:** a disallowed model/tool request is blocked at the proxy, never reaches upstream, and appears in `crabwise audit` as `outcome=blocked`.

## User Stories

1. **As a dev**, I point a compatible agent at `127.0.0.1:9119` and continue normal usage through a transparent proxy (with OpenAI-compatible payloads supported first).
2. **As a dev**, disallowed requests are denied immediately by policy without being forwarded upstream.
3. **As a dev**, I can see model, token, finish reason, provider, and cost telemetry in the audit trail.
4. **As a security-conscious dev**, sensitive outbound payload content is redacted before upstream egress when configured.
5. **As an operator**, queue pressure and overflow behavior are deterministic and observable under proxy load.
6. **As a Claude Code user**, existing log-watcher coverage remains intact and taxonomy-based commandments continue to match Claude tool activity consistently with proxy-captured events.

## Goals

1. Add a production-safe HTTP proxy framework with provider transport plug-in points.
2. Evaluate commandments in the request hot path and enforce `block` before forwarding upstream.
3. Capture token and finish telemetry from non-streaming and SSE streaming responses.
4. Add configurable per-model pricing and expose `crabwise audit --cost`.
5. Persist oversized/unknown payloads into bounded sidecar `.zst` blobs by logical event ID reference.
6. Define transport contracts so additional provider adapters can be added without changing enforcement/audit core paths.
7. Preserve Claude Code compatibility with no parser/classification/commandment-regression during proxy integration.

## Non-Goals (M2)

- No provider-specific retry logic or automatic failover across upstreams.
- No UI/TUI work beyond existing CLI surfaces.
- No monthly billing/accounting features; M2 is event-level cost telemetry plus CLI summary.
- No changes to third-party log files (redaction applies only to Crabwise persistence and proxy egress payloads).
- No commitment to ship multiple transport implementations in M2; OpenAI-compatible is transport v1, architecture is multi-provider.
- No hard block for pure log-watcher-only adapters; preventive block applies only on enforce-capable paths (proxy and future inline adapters).

## Locked Decisions

1. **Proxy bind:** `127.0.0.1:9119` by default, configurable in `adapters.proxy.listen`.
2. **Transport strategy:** provider-agnostic proxy core plus one production transport implementation in M2 (`openai`), including streaming and non-streaming paths.
3. **Single policy gate:** commandments evaluate only against canonical normalized request/response events; provider-specific code never contains policy decisions.
4. **Declarative normalization first:** onboarding a provider should be mapping-spec driven (selectors + transforms) before adding custom code.
5. **Enforcement point:** evaluate commandments after normalize/classification but before upstream forwarding.
6. **Blocked response contract:** HTTP `403` with stable JSON error shape (`policy_violation`) and no upstream request attempt.
7. **Commandment semantics:** existing engine remains canonical; proxy uses same match fields (`tool_category`, `tool_effect`, `model`, `arguments`, etc.).
8. **Redaction order:** evaluate -> enforce -> redact egress (if forwarding) -> send upstream.
9. **Raw payload reference:** DB stores logical event ID in `raw_payload_ref`; filesystem path is never accepted from external input.
10. **Queue behavior:** bounded queue remains mandatory; overflow emits `pipeline_overflow` system events with deterministic counters.
11. **Cost source:** usage from provider responses; cost derived from config pricing table (input/output per 1M tokens).
12. **Failure mode:** parse/telemetry/mapping-execution failures degrade to passthrough + `outcome=failure|success` audit event with `mapping_degraded` marker, never proxy panic. When `mapping_strict_mode=true`, mapping execution failures return HTTP 502 instead of passthrough.
13. **Claude coverage in M2:** Claude Code remains supported via log watcher with shared taxonomy and commandment evaluation; enforcement remains warn-only on non-enforcing adapters.
14. **Credential model:** each provider config declares `auth_mode`: `passthrough` (forward client's auth header verbatim) or `configured` (proxy supplies credential from config/env ref). Default is `passthrough`. Transport implementations handle header format differences (e.g. `Authorization: Bearer` vs `x-api-key`).
15. **Provider routing precedence:** explicit `X-Crabwise-Provider` header > path-pattern match (from per-provider `route_patterns`) > `default_provider` from config. Unroutable requests return HTTP 400 with stable error shape.
16. **Request ID contract:** proxy generates a UUID event ID per inbound request, propagates it as `X-Request-ID` to upstream (preserving any existing client value as `X-Crabwise-Original-Request-ID`), and returns it in all proxy responses including 403 blocked errors.
17. **Request body limit:** configurable `max_request_body` (default 10MB); requests exceeding the limit are rejected with HTTP 413 before any processing.
18. **Streaming timeout model:** `upstream_timeout` applies to connection + first-byte. A separate `stream_idle_timeout` (default 120s) governs maximum silence between SSE chunks during an active stream.

## Architecture Hook

```text
client request
  -> assign event ID (UUID), set X-Request-ID
  -> route to provider transport (header > path pattern > default)
  -> check request body size limit
  -> declarative mapping normalize (request -> canonical schema)
  -> optional provider-specific transform hook (mapping-layer escape hatch, not a Transport method; registered per-provider in mapping spec when declarative selectors are insufficient)
  -> classify tool intents (when present)
  -> commandments evaluate
      -> block: emit blocked audit event, return 403 with event ID, STOP
      -> warn/allow: continue
  -> egress redaction (if enabled)
  -> transport prepares auth (passthrough or configured credential, header format per provider)
  -> upstream call (pooled HTTP client per provider, timeout propagation)
  -> stream/non-stream telemetry extraction
  -> declarative mapping normalize (response -> canonical schema, including error responses)
  -> emit audited ai_request event (tokens/cost/finish/provider/error_type)
```

This keeps enforcement preventative (pre-upstream), while audit integrity remains centralized in the existing logger/hash chain pipeline. The response normalization path ensures error responses from different providers produce consistent audit events.

## Data Contract Changes

1. Reuse existing `AuditEvent` telemetry fields:
   - `provider`, `model`, `input_tokens`, `output_tokens`, `cost_usd`, `tool_*`, `classification_source`
2. Add proxy action conventions:
   - `action_type=ai_request`
   - `adapter_type=proxy`
   - `action` includes endpoint family (for example `responses.create`, `chat.completions.create`)
3. Canonical normalized policy input:
   - `provider`, `endpoint`, `model`, `stream`
   - `tools[]` entries with `{name, raw_args, arg_keys, tool_category, tool_effect}`
   - normalized text/arguments fields for existing matcher compatibility
4. Add stable commandment metadata on all proxy events:
   - deterministic `commandments_evaluated`, `commandments_triggered`
5. Normalized error response metadata (populated when upstream returns non-2xx, stored as structured keys in `Arguments` JSON — not new DB columns — to avoid schema migration and hash chain churn for fields that aren't primary query dimensions yet; `outcome=failure` remains the indexed filter):
   - `error_type` (normalized: `rate_limit`, `overloaded`, `invalid_request`, `auth`, `server_error`, `unknown`)
   - `error_message` (provider error message, redacted if configured)
   - `upstream_status` (raw HTTP status code from provider)
   - `mapping_degraded` (bool, present when mapping execution failed and proxy fell through to passthrough)
6. Raw payload sidecar:
   - `raw_payload_ref=<event-id>` when sidecar blob exists
   - compressed file path resolved internally as `<raw_payload_dir>/<event-id>.zst`

## Config Additions

`configs/default.yaml` and `internal/daemon/config.go`:

- `adapters.proxy.enabled` (bool)
- `adapters.proxy.listen` (host:port)
- `adapters.proxy.default_provider` (string, used when routing cannot determine provider)
- `adapters.proxy.upstream_timeout` (duration, connection + first-byte timeout)
- `adapters.proxy.stream_idle_timeout` (duration, max silence between SSE chunks, default 120s)
- `adapters.proxy.max_request_body` (bytes, default 10MB)
- `adapters.proxy.redact_egress_default` (bool)
- `adapters.proxy.mappings_dir` (directory with provider mapping specs)
- `adapters.proxy.mapping_strict_mode` (fail-closed on mapping execution errors vs passthrough/degrade)
- `adapters.proxy.providers` map — per-provider config:

```yaml
adapters:
  proxy:
    enabled: true
    listen: 127.0.0.1:9119
    default_provider: openai
    upstream_timeout: 30s
    stream_idle_timeout: 120s
    max_request_body: 10485760  # 10MB
    redact_egress_default: false
    mappings_dir: proxy_mappings/
    mapping_strict_mode: false
    providers:
      openai:
        upstream_base_url: https://api.openai.com
        auth_mode: passthrough        # passthrough | configured
        # auth_key: env:OPENAI_API_KEY  # only when auth_mode=configured
        route_patterns:
          - /v1/chat/completions
          - /v1/responses
          - /v1/embeddings
        max_idle_conns: 10
        idle_conn_timeout: 90s
```

- `cost.pricing` map keyed by model (`input`, `output` prices per 1M tokens)

Validation:
- listen address required when proxy enabled
- upstream_timeout > 0, stream_idle_timeout > 0
- max_request_body > 0
- each provider must have upstream_base_url and at least one route_pattern (or be the default_provider)
- auth_mode must be `passthrough` or `configured`; `configured` requires auth_key
- pricing entries non-negative
- mapping specs compile at startup/reload; on failure keep previous active mappings

## New Components

1. `internal/adapter/proxy/proxy.go`
   - server lifecycle, route registration, request ID/event ID plumbing
2. `internal/adapter/proxy/provider.go`
   - provider transport runtime interface (`PrepareAuth`, `Forward`, `ParseStreamEvent`)
3. `internal/adapter/proxy/router.go`
   - provider selection and transport registry
4. `internal/adapter/proxy/mapping.go`
   - declarative mapping compiler/executor (request/response/stream -> canonical schema)
5. `internal/adapter/proxy/openai.go`
   - first transport implementation for OpenAI-compatible wire protocol + mapping spec
6. `internal/adapter/proxy/streaming.go`
   - SSE passthrough with chunk-safe parsing and first-token timing instrumentation
7. `internal/adapter/proxy/redaction.go`
   - outbound JSON redaction for configured sensitive patterns
8. `internal/adapter/proxy/cost.go`
   - deterministic cost computation from pricing map + usage tokens
9. `internal/audit/rawpayload.go` (or equivalent package)
   - zstd sidecar write/read/quota/GC utilities
10. `configs/proxy_mappings/`
    - versioned provider mapping files (OpenAI v1 in M2)
11. `docs/design/mapping-spec.md`
    - mapping spec schema documentation (T0 output, defines the contract for adding providers)
12. `docs/design/transport-validation.md`
    - T0 multi-provider validation log (documents which Anthropic/Google differences the abstraction covers vs defers)

## Existing Components to Extend

1. `internal/daemon/daemon.go`
   - start/stop proxy adapter
   - inject commandments service + classifier + queue path into proxy runtime
2. `internal/daemon/config.go`
   - add proxy and cost config structures + validation
3. `internal/cli/audit.go`
   - add `--cost` summary mode
4. `internal/audit/query.go`
   - add cost summary query helpers (group by model/agent/day)
5. `internal/ipc/server.go` / daemon IPC handlers
   - add audit cost summary method (if CLI uses dedicated IPC endpoint)
6. `configs/default.yaml`
   - default proxy/cost settings

## Execution Plan

### Phase 0: Contracts and Scaffolding (sequential)

#### T0: Mapping spec design + transport interface validation
- Design the mapping spec YAML schema (field selectors, extraction primitives, transform functions, request/response/stream sections).
- Paper-walkthrough the mapping spec against OpenAI and Anthropic wire formats to validate provider agnosticism before implementation. Document which Anthropic differences the spec can handle declaratively vs which would require a custom transform hook (a named Go function registered in the mapping spec, invoked after declarative extraction — not part of the Transport interface).
- Define the transport interface (`Transport`) contract: `PrepareAuth(req)`, `Forward(ctx, req) (resp, error)`, `ParseStreamEvent([]byte) (event, error)`. Routing is owned by the router using config-driven `route_patterns`, not by transports — transports never decide which requests they handle.
- Validate the transport interface accommodates known provider differences (auth headers, streaming event shapes, error response structures) without baking in OpenAI assumptions.
- Produce: mapping spec schema doc, transport interface Go type (even if stub), and a short validation log noting which Anthropic/Google differences are covered by the abstraction and which are deferred.

#### T1: Config and daemon wiring
- Add proxy + cost config structs and validation (including per-provider auth_mode, route_patterns, connection pool settings).
- Wire proxy lifecycle into daemon startup/shutdown.
- Add `/health` endpoint to proxy listener; report proxy state in `crabwise status`.
- Ensure proxy is optional and does not regress log-watcher path.

#### T2: Proxy event contract
- Define internal request context model for event ID, endpoint, provider, model, tool intents, stream flag, start time, request ID.
- Standardize blocked audit event shape and error response payload (include event ID in 403 body).
- Define canonical normalized schema used by the single policy gate.
- Define error response canonical schema (`error_type`, `error_message`, `upstream_status`).

### Phase 1: Proxy Core (parallel-friendly)

#### Stream A: Forwarding + Enforcement

#### T3: HTTP proxy server
- Implement provider-neutral listener, routing, and forwarding lifecycle with context cancellation and timeout propagation.
- Request ID generation (UUID) and `X-Request-ID` header propagation on all responses.
- Request body size enforcement (`max_request_body`); reject with HTTP 413 before parsing.
- Maintain a pooled `http.Client` per provider transport (configurable `max_idle_conns`, `idle_conn_timeout`); upstream connections are HTTPS with keep-alive.
- Preserve transparent headers where safe (`Content-Type`, trace headers); auth headers handled by transport `PrepareAuth`.

#### T4: Transport interface and router
- Define transport contract: `PrepareAuth(req)`, `Forward(ctx, req) (resp, error)`, `ParseStreamEvent([]byte) (event, error)`. Transports handle auth, forwarding, and stream parsing — they do not participate in routing decisions.
- Implement transport registry: transports register by provider name at startup; registry is immutable after init.
- Implement provider routing (owned entirely by the router, not transports): resolve provider from `X-Crabwise-Provider` header (explicit) > path-pattern match against config-driven `route_patterns` per provider > `default_provider`. Return HTTP 400 with stable error shape on unroutable requests. Once resolved, dispatch to the registered transport for that provider.

#### T5: Declarative mapping engine
- Implement mapping spec loader/validator/executor for canonical request/response normalization.
- Compile mappings at startup/reload with atomic swap (same safety posture as commandments/tool registry).
- Support extraction primitives required for M2 (`model`, `tools`, `stream`, usage, finish reason, input summary, error type/message).
- Runtime mapping execution failures: emit `mapping_degraded` marker on audit event and pass through (default), or return 502 when `mapping_strict_mode=true`.

Mapping spec structure (per provider, versioned YAML):

```yaml
# configs/proxy_mappings/openai.yaml
version: "1"
provider: openai
request:
  model:         { path: "$.model" }
  stream:        { path: "$.stream", default: false }
  tools:
    path: "$.tools"
    each:
      name:      { path: "$.function.name" }
      raw_args:  { path: "$.function.parameters", serialize: json }
  input_summary: { path: "$.messages[-1].content", truncate: 200 }
response:
  model:         { path: "$.model" }
  finish_reason: { path: "$.choices[0].finish_reason" }
  usage:
    input_tokens:  { path: "$.usage.prompt_tokens" }
    output_tokens: { path: "$.usage.completion_tokens" }
  error:
    error_type:    { path: "$.error.type", map: { rate_limit_exceeded: rate_limit, server_error: server_error } }
    error_message: { path: "$.error.message" }
stream:
  terminal_event: { match: "$.usage != null" }
  usage:
    input_tokens:  { path: "$.usage.prompt_tokens" }
    output_tokens: { path: "$.usage.completion_tokens" }
  finish_reason:   { path: "$.choices[0].finish_reason" }
```

Extraction primitives: `path` (JSON selector using [gjson](https://github.com/tidwall/gjson) syntax — not RFC 9535 JSONPath — to pin selector semantics to a single well-defined Go library; e.g. `usage.prompt_tokens`, `choices.0.finish_reason`, `tools.#.function.name`), `default` (fallback value), `truncate` (max length), `serialize` (encoding), `map` (value translation), `each` (iterate array elements). The mapping spec examples above use `$.` prefix notation for readability; actual compiled selectors use gjson dot-notation.

#### T6: Pre-forward commandment enforcement
- Build preflight `AuditEvent` candidate from request payload.
- Run commandments evaluate before upstream call.
- On block: enqueue blocked event and return 403 without forwarding.
- On warn/allow: continue to redaction/forward.

#### T7: OpenAI transport + mapping (v1)
- Implement OpenAI wire transport: `PrepareAuth` for `Authorization: Bearer` header format; `Forward` with pooled HTTPS client; `ParseStreamEvent` for OpenAI SSE `data:` frames. Route patterns (`/v1/chat/completions`, `/v1/responses`, `/v1/embeddings`) configured in provider config, resolved by the router.
- Ship `configs/proxy_mappings/openai.yaml` mapping spec into canonical schema.
- Apply central classifier to tool operations when present.
- Set provider/tool taxonomy fields consistently with M1.5.
- Normalize OpenAI error responses (429 → `rate_limit`, 500/503 → `server_error`, 400 → `invalid_request`, 401 → `auth`).

#### Stream B: Streaming + Telemetry

#### T8: SSE passthrough and parser
- Implement chunk-safe SSE read/write path with flush.
- Handle partial event frames, multi-event chunks, keep-alives, upstream disconnect, client cancel, timeout.
- Enforce `stream_idle_timeout` between chunks; close connection on idle expiry with audit event.
- Maintain transparent passthrough behavior when parser cannot decode an event.

#### T9: Non-streaming telemetry extraction
- Parse completion response JSON for usage + finish reason + resolved model.
- Emit audited ai_request event with computed cost.

#### T10: Streaming telemetry extraction
- Extract usage from terminal stream events when available.
- Capture first-token latency delta and completion metadata.
- Emit single consolidated audited ai_request event at request completion.

#### Stream C: Redaction + Raw Payload

#### T11: Proxy egress redaction
- Redact configured sensitive patterns in outbound payload body before upstream forwarding.
- Add `egress_redacted` marker in event arguments/metadata when applied.
- Ensure redaction is deterministic and bounded.

#### T12: Raw payload sidecar storage
- Implement zstd blob writer and bounded retention/quota GC.
- Enforce per-payload max size and truncation marker.
- Store only logical `raw_payload_ref` in DB event row.

### Phase 2: Cost and CLI Integration (sequential)

#### T13: Pricing + cost computation
- Add per-model pricing map to config.
- Compute `cost_usd` from input/output tokens.
- Handle unknown model pricing by emitting `cost_usd=0` plus audit marker.

#### T14: `crabwise audit --cost`
- Add CLI mode to print spend summaries by model and agent.
- Ensure output works with existing filters (`--since`, `--until`, `--agent`).

### Phase 3: Hardening and Release Gates (sequential)

#### T15: Queue overflow + proxy observability under load
- Validate queue depth/drop accounting under sustained proxy traffic.
- Ensure `pipeline_overflow` events remain emitted and queryable.
- Add proxy runtime metrics to `crabwise status`: active connections, total requests served, total blocked, upstream error count, mapping degradation count.

#### T16: Latency/SSE gate benchmarks
- Measure proxy overhead and first-token delta with the standard benchmark profile.
- Tune buffering/flush behavior to satisfy SLO gates.

#### T17: Claude parity and conformance gate
- Replay Claude Code fixtures and assert no parser/classifier regressions.
- Run cross-adapter conformance assertions (Claude log watcher vs proxy transport fixtures) for taxonomy/classification consistency.
- Verify enforcement capability semantics remain explicit: identical rule intent yields `warn` on log watcher and `block` on proxy path.

## Files Changed (Planned)

| File | Change |
|------|--------|
| `internal/adapter/proxy/proxy.go` | New proxy server runtime (listener, request ID, body limit, health endpoint) |
| `internal/adapter/proxy/provider.go` | New provider transport interface (`PrepareAuth`, `Forward`, `ParseStreamEvent`) |
| `internal/adapter/proxy/router.go` | New provider routing/registry (router owns routing via config `route_patterns`; header > path pattern > default) |
| `internal/adapter/proxy/mapping.go` | Declarative mapping compile/execute engine (request/response/stream sections) |
| `internal/adapter/proxy/openai.go` | OpenAI transport v1 implementation (auth, routing, error normalization) |
| `internal/adapter/proxy/streaming.go` | New SSE passthrough + parsing (idle timeout, chunk-safe) |
| `internal/adapter/proxy/redaction.go` | New outbound redaction pipeline |
| `internal/adapter/proxy/cost.go` | New cost computation |
| `internal/adapter/proxy/*_test.go` | New unit/integration tests (includes stub provider conformance) |
| `configs/proxy_mappings/openai.yaml` | OpenAI mapping spec to canonical schema |
| `internal/audit/rawpayload.go` | New sidecar payload manager |
| `internal/audit/rawpayload_test.go` | New payload quota/GC tests |
| `internal/daemon/config.go` | Proxy + cost config model/validation (per-provider auth, routes, pool settings) |
| `internal/daemon/daemon.go` | Proxy lifecycle + runtime wiring |
| `internal/audit/query.go` | Cost summary query support |
| `internal/cli/audit.go` | `--cost` output mode |
| `internal/cli/status.go` | Proxy runtime metrics in status output |
| `configs/default.yaml` | Proxy + cost defaults |
| `docs/design/mapping-spec.md` | Mapping spec schema documentation (T0 output) |
| `docs/design/transport-validation.md` | T0 multi-provider validation log |
| `testdata/proxy/*` | Proxy fixtures (streaming + non-streaming + blocked + error responses) |
| `testdata/proxy/testprovider/*` | Stub provider conformance test fixtures |
| `internal/adapter/logwatcher/conformance_test.go` | Extend Claude/proxy conformance coverage |
| `testdata/claude-code/*` | Reused for Claude parity regression coverage |

## Testing Plan

### Unit Tests

- Cost calculation (known-answer tests, unknown model pricing fallback).
- Redaction deterministic replacements and bounds.
- Raw payload truncation, quota GC, path safety.
- Commandment preflight block decision logic in proxy hot path.
- Mapping compiler/executor tests (selector resolution, transforms, fallback behavior, runtime extraction failure → degraded marker).
- Provider routing tests (header precedence > path match > default, unroutable → 400).
- Request body limit enforcement (under/at/over limit).

### Integration Tests

- Blocked requests never hit upstream mock server.
- Allowed requests pass through and emit auditable telemetry.
- `commandments_evaluated` / `commandments_triggered` serialized deterministically for proxy events.
- Transport conformance tests: all transports must satisfy shared blocked/enforce/audit contract.
- **Stub provider conformance test:** a minimal `testprovider` transport (echo server + trivial mapping spec) registered via the standard path, passing the full blocked/enforce/audit contract. Validates the exit gate that adding a provider requires only mapping spec + transport registration.
- Claude parity tests: existing Claude fixtures still classify and trigger intended commandments after proxy-core changes.
- Mapping conformance tests: provider mappings produce canonical schema required by the single policy gate.
- Error response normalization: upstream 4xx/5xx responses produce consistent `error_type`/`error_message` audit fields across providers.
- Request ID propagation: `X-Request-ID` present in all proxy responses (success, blocked, error).

### Streaming Torture Suite

- Partial chunks split mid-SSE line.
- Multi-event chunks in one read.
- Client cancellation during stream.
- Upstream timeout (connection + first-byte).
- Stream idle timeout (no chunks for > `stream_idle_timeout`).
- Upstream disconnect after N chunks.
- Empty keep-alive frames.
- Malformed SSE lines (passthrough fallback, no panic).

### Performance and Reliability

- Proxy added latency gate: p95 < 20ms.
- First-token delta gate: < 50ms.
- Queue overflow deterministic under 100 events/sec sustained load.

## Exit Gates

- [ ] Blocked requests are never forwarded upstream (asserted by integration tests).
- [ ] Proxy overhead p95 < 20ms on benchmark profile.
- [ ] Streaming first-token delta < 50ms.
- [ ] Streaming torture suite passes without panics or deadlocks.
- [ ] Cost telemetry populated for priced models and surfaced in `crabwise audit --cost`.
- [ ] Egress redaction tests pass; original third-party logs remain unchanged.
- [ ] Raw payload limits/quota/GC verified; no path-injection vector in payload refs.
- [ ] Queue overflow events/counters remain observable under proxy load.
- [ ] Claude Code path has zero regression in parser/classifier behavior and retains expected warn-only enforcement semantics.
- [x] Adding a provider requires only mapping spec + transport registration (no new policy gate code path) — validated by transport registry pattern (`RegisterTransport` + `init()`).
- [x] Provider routing resolves correctly via header > path pattern > default precedence (asserted by unit tests).
- [x] Upstream error responses produce normalized `error_type`/`error_message` audit fields.
- [x] `X-Request-ID` present on all proxy responses and correlates to audit event ID.
- [x] T0 validation log documents which Anthropic/Google differences are covered by the transport abstraction and which are deferred.

## Execution Checklist (Implementation Order)

- [x] **T0:** Design mapping spec schema and validate transport interface against OpenAI + Anthropic wire formats on paper.
- [x] **T0:** Produce mapping spec schema doc + transport validation log.
- [x] **T1:** Add proxy + cost config schema/defaults/validation (per-provider auth, routes, pool, timeouts).
- [x] **T1:** Scaffold `internal/adapter/proxy` package, daemon lifecycle wiring, and `/health` endpoint.
- [x] **T2:** Define proxy event contract, blocked response shape, error response canonical schema.
- [x] **T4:** Define provider transport interface (`PrepareAuth`/`Forward`/`ParseStreamEvent`) and config-driven routing registry (header > path > default; router owns routing, not transports).
- [x] **T5:** Implement declarative mapping loader/compiler/executor and canonical schema contract.
- [x] **T6:** Implement preflight request parse + classification + commandment evaluation.
- [x] **T6:** Implement block response contract and blocked event audit emission (with event ID in 403).
- [x] **T7:** Implement OpenAI transport + mapping v1 upstream passthrough for non-streaming responses.
- [x] **T8:** Implement SSE passthrough parser with chunk-safe handling and idle timeout.
- [x] **T9/T10:** Add non-streaming + streaming telemetry extraction and event emission.
- [x] **T11:** Add outbound proxy redaction with deterministic behavior markers.
- [x] **T12:** Implement raw payload `.zst` sidecar manager and retention/quota GC.
- [x] **T13:** Add cost pricing config and `cost_usd` computation.
- [x] **T14:** Add IPC/query plumbing and `crabwise audit --cost`.
- [ ] Add proxy fixtures + integration + streaming torture tests.
- [ ] Add stub provider conformance test (validates provider-agnosticism exit gate).
- [x] **T15:** Add proxy runtime metrics to `crabwise status`.
- [ ] **T17:** Run Claude fixture regression + cross-adapter conformance gates.
- [ ] Run full test suite and benchmark gates; block merge until all exit gates pass.

## Post-Implementation Changes (adjustments beyond original plan)

1. **Transport self-registration pattern:** Providers register via `init()` + `RegisterTransport()` instead of a hardcoded switch in proxy core. Adding a provider requires zero changes to `proxy.go` — only a new file with `init()` and a mapping YAML.
2. **gjson for selector semantics:** Mapping engine uses `github.com/tidwall/gjson` directly on raw JSON bytes instead of unmarshaling to `interface{}` and walking the tree. More correct, faster, and pins selector behavior to a single well-defined library.
3. **Negative index translation:** `toGjsonPath` converts `[-1]` to gjson's `@reverse.0` modifier. gjson does not support negative array indexes natively — without this, `$.messages[-1].content` (used for `input_summary`) silently returned empty.
4. **Config-time regex validation:** `redact_patterns` entries are validated at config load via `regexp.Compile`. `CompilePatterns` returns an error on first invalid regex rather than silently dropping it. Bad patterns fail at startup, not at request time.
5. **Response strict mode buffering:** Non-streaming response body is buffered before writing to the client, allowing `mapping_strict_mode=true` to return HTTP 502 on response mapping failure. Previously, headers/body were sent before normalization, making fail-close impossible.
6. **Response mapping failure sets outcome=failure:** Both strict and non-strict paths now set `normResp.ErrorType = "mapping_error"` when response normalization fails, ensuring the audit event records `outcome=failure` instead of `outcome=success` for a 2xx upstream with broken normalization.
7. **First-token latency precision:** `first_token_ms` is captured only after SSE `event:` and `data:` line parsing, so it fires on the first actual data payload rather than an `event:` type line. Prevents under-reporting for providers that emit `event:` before `data:`.
8. **SSE `event:` line capture:** `parseSSEEventType` extracts the SSE `event:` field alongside `data:`, stored on `StreamEvent.EventType`. Required for future providers (e.g. Anthropic's `content_block_delta` events).
9. **`cost_unknown_model` marker:** `ComputeCost` returns a structured `CostResult` with `UnknownModel bool` flag. Unpriced models produce `cost_usd=0` plus `cost_unknown_model=true` in audit Arguments JSON — operators can detect pricing gaps.
10. **Multi-tool persistence:** When a request contains >1 tool, all tools are serialized into an Arguments JSON `"tools"` array with name/category/effect per tool. Previously only the first tool was surfaced on the top-level event fields.
11. **Atomic mapping reload on SIGHUP:** `proxy.ReloadMappings()` rebuilds all provider runtimes (transports + specs) and atomically swaps the router, wired into the daemon's SIGHUP reload path with audit events for success/failure.
12. **`RawPayloadWriter` interface:** Proxy accepts raw payload storage via a `RawPayloadWriter` interface rather than coupling directly to `audit.RawPayloadManager`, enabling testing and alternative storage backends.
13. **Bounded egress redaction:** Redaction capped at 50 replacements per field (`maxRedactionsPerField`) to prevent pathological regex amplification on large payloads.
14. **`klauspost/compress/zstd`:** Raw payload sidecar uses `github.com/klauspost/compress/zstd` (pure Go, well-maintained) instead of the nonexistent `compress/zstd` stdlib package.

## What This Enables for M3

1. TUI can render real-time blocked/warned proxy governance events with cost counters.
2. OTel export can emit complete GenAI spans with reliable usage/cost dimensions.
3. Release hardening benefits from already-verified streaming correctness and enforcement behavior.
4. Adding Anthropic or Google transport requires only a new mapping spec YAML + transport registration — the T0 validation log documents exactly which provider differences are already accommodated by the abstraction.

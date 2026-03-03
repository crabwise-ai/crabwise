# Crabwise AI — Prototype Plan

## Context

- **Architecture:** `plan/crabwise-prototype-architecture-plan.md`
- **Implementation design:** `docs/plans/2026-02-22-prototype-implementation-design.md`

## What We're Building

Single Go binary (`crabwise`) — local-first daemon + CLI/TUI that monitors AI agent activity, enforces YAML-defined commandments at infrastructure level, and maintains a tamper-evident audit trail. Targets Claude Code (log watcher) and OpenAI-compatible agents (proxy) as first two adapters.

## Top User Stories

1. Install one command, immediately see what Claude Code is doing — no config changes
2. YAML rules like "never run rm -rf" enforced at infrastructure layer, can't be jailbroken
3. Searchable, exportable audit trail of every agent action with token usage
4. Warnings on sensitive file access, ability to block destructive commands

## Key Architecture Decisions

- **Go single binary** — daemon + Crabwalk CLI/TUI (Cobra + Bubble Tea)
- **SQLite** (`modernc.org/sqlite`, pure Go) — local-first, zero ops, WAL mode
- **Two adapter tiers** — Log Watcher (read-only, Claude Code) + Proxy (enforce, OpenAI-compatible on :9119)
- **Commandment engine** — YAML rules, regex/glob matching, block/warn enforcement, <2ms p95 eval
- **IPC** — JSON-RPC 2.0 over Unix socket
- **Hash chain** — SHA-256 over full redacted event record, daily epoch anchors for retention-aware verification
- **Event queue** — bounded channel (10k), batched SQLite writes, default block_with_timeout, overflow metered
- **Socket auth** — SO_PEERCRED kernel-verified peer credentials + file permissions
- **OTel GenAI spans** — optional collector, local-first by default
- **Redaction** — both paths: audit persistence AND proxy egress. Never mutates third-party logs.
- **Raw payloads** — sidecar `.zst` blobs, logical ID ref, 1MB max/500MB quota, hourly GC
- **Proxy egress redaction** — deterministic order: detect → block|warn → redact-if-allowed → forward

## Milestones (Vertical Slices)

Each milestone delivers a demo-able user story.

- **M0 (Weeks 1-1.5)** — Daemon + CC log watcher + SQLite audit + basic watch
  - Demo: `crabwise start && crabwise audit` shows CC events
- **M1 (Weeks 2-2.5)** — Commandment engine + warn enforcement + audit redaction + SIGHUP reload
  - Demo: `.env` access triggers warning in `crabwise audit --triggered`
- **M2 (Weeks 3-4)** — HTTP proxy + SSE streaming + block enforcement + GenAI telemetry + cost tracking
  - Demo: blocked model denied before reaching provider
- **M3 (Week 5)** — Bubble Tea TUI + OTel export + install script + cross-compile + benchmarks
  - Demo: `crabwise watch` shows live TUI with agent status, cost, warnings

## SLOs (Release Gates)

| Area | Gate |
|------|------|
| Commandment eval | p95 < 2ms, p99 < 8ms |
| Proxy overhead | p95 < 20ms |
| Streaming first-token | delta < 50ms |
| Event loss | 0 under nominal load; overflow metered |
| Daemon footprint | RSS < 80MB |
| Security | Redaction passes on both surfaces; blocked actions never reach upstream |

## Verification

1. Agent discovery — `crabwise agents` shows Claude Code
2. Log watcher — `crabwise audit` shows CC tool calls, commands, AI requests
3. Parser safety — malformed/unknown records captured via raw_payload, no panics
4. Commandment warn — `.env` access triggers warning in audit
5. Proxy block — disallowed model denied before reaching provider
6. Streaming — SSE correct under chunking, disconnect, cancel, timeout
7. Token usage — `crabwise audit --tokens` shows input/output tokens by agent/day (cost tracking is a future feature)
8. Audit integrity — `crabwise audit --verify-integrity` validates chain
9. Redaction — tested on both audit persistence and proxy egress paths
10. Performance — benchmark suite confirms all SLOs
11. TUI — `crabwise watch` shows live feed, warnings, queue depth, drop counters

## Resolved Questions

1. Module path: `github.com/crabwise-ai/crabwise`
2. CC logs: M0 gated on fixture capture + anonymization script
3. Cost pricing: deferred — token usage tracked; USD cost calculation is a future feature
4. OpenClaw: proxy-first only, no log watcher in prototype
5. Systemd: optional via `crabwise install --service`
6. License: MIT — single-player open source, team features are future commercial surface

## De-scope Fallback (if week 5 slips)

Defer in order: TUI filters → OTel → install script → arm64 → cost summary view.
**Never de-scope:** proxy correctness, SSE streaming, block enforcement, audit integrity, redaction.

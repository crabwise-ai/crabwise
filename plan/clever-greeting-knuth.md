# Crabwise AI — Prototype Plan

## Context

Canonical plan lives at `/home/del/Github/crabwise/plan/crabwise-prototype-architecture-plan.md`. This file is a summary reference.

## What We're Building

Single Go binary (`crabwise`) — local-first daemon + CLI/TUI that monitors AI agent activity, enforces YAML-defined commandments at infrastructure level, and maintains a tamper-evident audit trail. Targets Claude Code (log watcher) and OpenClaw (proxy) as first two adapters.

## Key Architecture Decisions

- **Go single binary** — daemon + Crabwalk CLI/TUI (Cobra + Bubble Tea)
- **SQLite** (`modernc.org/sqlite`, pure Go) — local-first, zero ops
- **Two adapter tiers** — Log Watcher (read-only, Claude Code) + Proxy (enforce, OpenAI-compatible on :9119)
- **Commandment engine** — YAML rules, regex/glob matching, block/warn enforcement, <2ms p95 eval
- **OTel GenAI spans** — optional collector, local-first by default
- **Audit integrity** — hash-chain events, `crabwise audit --verify-integrity`
- **Redaction** — both paths: audit persistence AND proxy egress. Does not mutate original third-party logs.
- **Raw payloads** — sidecar `.zst` blobs at `~/.local/share/crabwise/raw/<event-id>.zst`, GC tied to retention

## Milestones

- **M0 (Week 1)** — Skeleton + agent discovery + IPC socket
- **M0.5 (Week 1.5)** — Hardening baseline: threat model, redaction pipeline, bounded queue, benchmark harness, SIGHUP reload
- **M1 (Week 2)** — Claude Code log watcher + SQLite audit trail + hash-chain integrity
- **M2 (Week 3)** — Commandment engine (YAML parser, matchers, warn enforcement, priority/conflict resolution)
- **M3 (Week 4)** — HTTP proxy + SSE streaming + block enforcement + GenAI telemetry
- **M4 (Week 5)** — Bubble Tea TUI + OTel export + install script + cross-compile + README

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

1. Agent discovery — `crabwise agents` shows Claude Code + OpenClaw
2. Log watcher — `crabwise audit` shows CC tool calls, commands, AI requests
3. Parser safety — malformed/unknown records captured via raw_payload, no panics
4. Commandment warn — `.env` access triggers warning in audit
5. Proxy block — disallowed model denied before reaching provider
6. Streaming — SSE correct under chunking, disconnect, cancel, timeout
7. Audit integrity — `crabwise audit --verify-integrity` validates chain
8. Redaction — tested on both audit persistence and proxy egress paths
9. Performance — benchmark suite confirms all SLOs before milestone sign-off

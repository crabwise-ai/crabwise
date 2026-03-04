# Crabwise — Product Brief

*For marketing team use. Internal working document.*

---

## The problem

AI coding agents are powerful. They read your files, run shell commands, call APIs, and push code — often faster than any human can review. That speed is the point. It is also the risk.

Until now, most teams have had no idea what their AI agents actually did during a session. No log. No trail. No way to know if an agent tried to push to main, touched a credentials file, or called a model that was not approved. When something goes wrong, the audit starts from scratch.

And the problem is getting harder. AI agents are no longer just tools developers run interactively from a terminal. They are services — long-running daemons that orchestrate work autonomously, call AI providers in the background, and execute actions with no human in the loop. Governing those agents requires a different approach than governing a CLI command.

Security and compliance teams are asking the same question: how do we govern AI agents the same way we govern human engineers?

---

## What Crabwise does

Crabwise is a local-first agent monitoring and enforcement daemon. It sits between your AI agents and the outside world — watching everything they do, recording it to a tamper-evident audit log, and stopping dangerous actions before they happen.

It works with two classes of agents:

**Interactive agents** — tools developers run directly from a terminal: Claude Code, Codex CLI, and any agent that speaks the OpenAI API format. Crabwise wraps the launch command and routes traffic through its proxy automatically.

**Service-level agents** — long-running daemons managed by systemd or launchd: OpenClaw and other autonomous agent runtimes. Crabwise injects proxy configuration into the service definition so enforcement persists across reboots without any changes to the agent itself.

Both classes get identical governance: the same proxy, the same policy rules, the same audit trail.

No cloud dependency. No changes to your agents. One binary, one install.

---

## How it works

**1. Observe**

Crabwise watches your AI agent sessions as they run. It reads the structured logs that agents produce — and connects directly to service-level agent runtimes like OpenClaw via their WebSocket gateway — turning every tool call, file access, shell command, and AI request into a structured audit event.

Every event is written to a local SQLite database with a cryptographic hash chain. Each record links to the one before it. If anyone tampers with the log, the chain breaks. You can verify integrity at any time with a single command.

**2. Intercept**

Crabwise runs a local HTTPS proxy. All traffic from your agents to AI providers routes through it.

For interactive agents, launch through Crabwise once:
```bash
crabwise wrap -- claude
crabwise wrap -- codex
```

For service-level agents, inject proxy config into the service definition once:
```bash
sudo crabwise service inject --agent openclaw --restart
```

Either way, the proxy intercepts every AI provider call that agent makes — now and after every reboot.

Before any response reaches the agent, Crabwise buffers the full response, extracts the tool calls the model is about to instruct the agent to run, and evaluates them against your policy rules.

**3. Enforce**

Policy rules are called **commandments** — a YAML file you own and version-control. Rules match on any combination of: tool name, tool category, file paths, shell command content, AI model name, token usage, agent identity, and more.

When a rule matches:

- `warn` — the action proceeds, the event is flagged in the audit trail
- `block` — the request is stopped before it reaches the AI provider. The agent gets an HTTP 403. The action never happens upstream.

Rules hot-reload without restarting the daemon:
```bash
crabwise commandments reload
```

**4. Query**

A live terminal dashboard and full query interface over the audit trail. Filter by agent, session, outcome, time range, or which commandment triggered. Export to JSON. Verify the hash chain. Stream events as they happen.

---

## What you can enforce

Out of the box, the default install blocks destructive shell commands (`rm -rf`, `mkfs`, `dd if=`). Beyond that, you write the rules your organization needs:

- Block direct pushes to `main` or `master`
- Block access to credential files (`.env`, `*.pem`, `*credentials*`) — with optional redaction of event content so sensitive paths do not appear in the audit log
- Block requests to AI models not on your approved list
- Warn when a single request exceeds a token usage threshold
- Match on specific agent identities — block a rule for one agent without affecting others
- Combine multiple conditions in a single rule — tool category, file path, and model, all at once

---

## OpenClaw: service-level agent governance

OpenClaw is an autonomous agent runtime that runs as a system service. It orchestrates AI sessions, manages execution, and calls AI providers without a human initiating each request. That is its strength — and what makes governance harder.

Crabwise connects to the OpenClaw Gateway via WebSocket and observes sessions in real time: which model is being used, how many tokens each request consumes, what runs are active, and how sessions relate to each other. When an OpenClaw session makes an AI provider call, Crabwise correlates that call back to the originating session and enriches the audit event with full session context — agent ID, run ID, model, correlation confidence.

Proxy enforcement applies identically. If a commandment blocks the request, OpenClaw gets an HTTP 403 and the upstream call never happens. The blocked event is recorded with the session context attached.

For production deployments running OpenClaw under systemd or launchd, `crabwise service inject` modifies the service unit to inject proxy environment variables permanently. One command, survives reboots:
```bash
sudo crabwise service inject --agent openclaw --restart
```

---

## What Crabwise does not do

Crabwise is direct about its scope.

- **It cannot block shell commands that run locally.** When an AI agent executes `rm -rf` on your machine, that is a local process — not an API call. The proxy intercepts AI provider traffic, not shell execution. Local tool calls are captured in the audit log after the fact, and block rules downgrade to warnings for those events.
- **It does not inspect non-provider traffic.** Connections to GitHub, npm, or any other domain pass through without inspection.
- **It does not require changes to the agent.** No plugins, no SDK wrappers, no modifications to how the agent is built.
- **OpenClaw enforcement is at the provider-call level.** Crabwise blocks upstream AI requests — it does not intercept local tool execution inside the OpenClaw host process after a model response is already in flight.

---

## The audit trail

Every event Crabwise records includes:

- Timestamp, agent ID, session ID, working directory
- Action type: tool call, file access, command execution, AI request
- Tool name, category, and effect (read, write, execute)
- File paths and shell arguments
- AI provider, model name, input and output token counts
- For OpenClaw sessions: run ID, session key, correlation confidence
- Which commandments were evaluated and which triggered
- Outcome: success, warned, or blocked
- A cryptographic hash linking each event to the previous one

The hash chain makes the log tamper-evident — deletions or edits are detectable. Export the full trail as JSON for handoff to a SIEM, compliance system, or your own tooling.

---

## Integration

**OpenTelemetry** — Crabwise exports GenAI spans via OTLP HTTP to any collector: Datadog, Grafana, Honeycomb, or any OTEL-compatible backend. Spans follow the OpenTelemetry GenAI semantic conventions with Crabwise extensions for outcome and policy enforcement.

**Service management** — `crabwise service` manages proxy injection for agents running under systemd (Linux) or launchd (macOS), with system scope (root) and user scope (no root) variants.

**SIEM and compliance export** — `crabwise audit --export json` streams the full audit trail as newline-delimited JSON.

---

## Who it is for

**Security and compliance teams** — a tamper-evident audit trail for every AI agent action, and policy enforcement that stops violations before they happen. Same governance model you apply to human engineers.

**Platform and DevOps teams** — enforce approved model lists, token usage guardrails, and service-level agent governance across interactive and daemon-managed workloads from a single control point.

**Engineering teams** — full visibility into what your AI agents actually did. Filter, replay, and export session history. Catch runaway behavior before it causes an incident.

**Individual developers** — know exactly what your AI tools are doing. Verify every session, every tool call, every file touched.

---

## Installation

Single binary. Runs on Linux and macOS (Intel and Apple Silicon).

```bash
# Install
curl -sSfL https://raw.githubusercontent.com/crabwise-ai/crabwise/main/install.sh | bash

# Initialize config and generate CA certificate
crabwise init

# Trust the local CA (required for HTTPS interception)
crabwise cert trust --copy

# Start the daemon
crabwise start

# Wrap interactive agents
crabwise wrap -- claude
crabwise wrap -- codex

# Or inject into a service-level agent (survives reboots)
sudo crabwise service inject --agent openclaw --restart
```

The install script verifies a SHA-256 checksum. If verification fails, installation aborts.

---

## Licensing

AGPL-3.0.

---

## Key facts for marketing

| | |
|---|---|
| **What it is** | Local agent monitoring and policy enforcement daemon |
| **Agent types** | Interactive (Claude Code, Codex CLI) and service-level (OpenClaw, any OpenAI-compatible agent) |
| **How enforcement works** | HTTPS proxy intercepts LLM provider calls, evaluates tool blocks before forwarding |
| **Policy format** | YAML commandments — version-controlled, hot-reloadable, no restart required |
| **Audit storage** | Local SQLite, hash-chained for tamper evidence |
| **OpenClaw support** | WebSocket gateway integration for session attribution and service-level governance |
| **Service management** | systemd (Linux) and launchd (macOS) injection via `crabwise service` |
| **Observability export** | OpenTelemetry OTLP HTTP (GenAI semantic conventions) |
| **Cloud dependency** | None |
| **Code changes required** | None |
| **Platforms** | Linux, macOS (Intel + Apple Silicon) |
| **License** | AGPL-3.0 |
| **Install method** | Single binary, checksum-verified shell script installer |

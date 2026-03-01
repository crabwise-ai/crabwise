# Service Inject Design

Date: 2026-02-28
Status: approved for planning

## Summary

This design turns `crabwise service` into a general platform feature for governing daemon-managed agents across both system and user service domains.

The CLI keeps `system` as the default scope because OpenClaw and most production daemon installs live there, but the feature must support `user` scope equally well on both Linux and macOS.

## Problem Statement

Crabwise governs AI agent provider calls by routing them through a forward HTTPS proxy. For interactive CLI agents like Codex and Claude Code, `crabwise wrap` injects `HTTPS_PROXY` and `NODE_EXTRA_CA_CERTS` into the child process environment at exec time. This works because the user launches those agents from a shell and the agent inherits the modified environment.

This model breaks for **service-managed agents** — agents that run as system daemons under systemd (Linux) or launchd (macOS). OpenClaw is the first such agent. Its Gateway process is typically installed via `openclaw onboard --install-daemon`, which creates a systemd unit or launchd plist. The daemon process:

1. **Does not inherit shell environment.** Environment variables are baked into the service unit file at install time, not read from the user's shell.
2. **Is not a child of `crabwise wrap`.** The daemon is started by the init system (PID 1), not by a user shell. `crabwise wrap -- openclaw gateway` only works for interactive/dev mode, not production installs.
3. **Survives reboots.** The service unit persists across restarts, so proxy configuration must be durable, not session-scoped.

Without a mechanism to inject proxy environment variables into service unit files, Crabwise cannot intercept provider traffic from any daemon-managed agent. The agent's HTTPS calls bypass the proxy entirely and reach upstream APIs ungoverned.

## Applicability to Future Service-Level Agents

This problem is not specific to OpenClaw. Any autonomous AI agent that runs as a background service will have the same issue:

- **Self-hosted Devin** or similar autonomous coding agents deployed as system services
- **SWE-agent daemons** running continuous task loops under systemd
- **Custom autonomous agents** built on frameworks like AutoGen, CrewAI, or LangGraph, deployed as production services
- **MCP servers** that make outbound provider calls from a long-running daemon process
- **Any future agent** installed via a package manager or onboarding script that creates a service unit

The `crabwise service inject` command is designed to be agent-agnostic. It injects the same proxy environment variables into any named service unit. The only OpenClaw-specific detail is the default `--service` name. A user governing a future agent would run:

```bash
sudo crabwise service inject --service my-future-agent --restart
```

This makes Crabwise's governance model complete across both interaction patterns:

| Agent type | Proxy injection method | Example |
|---|---|---|
| Interactive CLI | `crabwise wrap -- <agent>` | Codex, Claude Code |
| System daemon | `sudo crabwise service inject --scope system --service <name>` | OpenClaw, future autonomous agents |
| User daemon | `crabwise service inject --scope user --service <name>` | Local dev agents, per-user MCP servers |

Both paths converge at the same forward proxy, commandment engine, and audit trail. The governance guarantees are identical regardless of how the agent was launched.

## Goals

- Support `crabwise service inject|remove|status` for both `system` and `user` scopes
- Keep one simple mental model across platforms:
  - `system` scope is privileged
  - `user` scope is unprivileged
- Inject the same proxy environment variables that `crabwise wrap` uses
- Make status truthful by checking for actual injected proxy env, not just file existence
- Avoid implicit fallback between scopes

## Non-Goals

- Supporting `/Library/LaunchAgents` in phase 1
- Auto-detecting scope by searching both domains and picking one
- Managing service installation, enablement, or bootstrapping

## Architecture Diagram

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                             USER'S SYSTEM                                   │
│                                                                             │
│  ┌───────────────────────────────────┐                                      │
│  │       Interactive Agents          │                                      │
│  │       (launched from shell)       │                                      │
│  │                                   │                                      │
│  │  $ crabwise wrap -- codex         │                                      │
│  │  $ crabwise wrap -- claude        │                                      │
│  │                                   │                                      │
│  │  HTTPS_PROXY injected via exec()  │                                      │
│  └──────────────┬────────────────────┘                                      │
│                 │                                                            │
│                 │  provider API calls                                        │
│                 ▼                                                            │
│  ┌────────────────────────────────────────────────────────┐                  │
│  │              Crabwise Daemon  (crabwise start)         │                  │
│  │                                                        │                  │
│  │  ┌──────────────┐  ┌─────────────────────────────────┐ │                  │
│  │  │ Forward HTTPS │  │ OpenClaw Gateway Adapter        │ │                  │
│  │  │ Proxy :9119   │  │ (read-only, ws://127.0.0.1:    │ │                  │
│  │  │               │  │  18789)                         │ │                  │
│  │  │ • intercept   │  │ • session cache                 │ │                  │
│  │  │ • decrypt     │  │ • run correlation               │ │                  │
│  │  │ • evaluate    │  │ • event enrichment              │ │                  │
│  │  │ • block/allow │  └────────────┬────────────────────┘ │                  │
│  │  └──────┬───────┘               │                      │                  │
│  │         │                       │                      │                  │
│  │         │  ┌────────────────────┴───────────────────┐   │                  │
│  │         │  │ Correlation Store (in-memory)          │   │                  │
│  │         │  │ runId → sessionKey → agentId/model     │   │                  │
│  │         │  └────────────────────────────────────────┘   │                  │
│  │         │         ▲ lookup                              │                  │
│  │         ▼         │                                     │                  │
│  │  ┌────────────────┴────────────────────────────────┐    │                  │
│  │  │ Commandment Engine                              │    │                  │
│  │  │ • evaluate rules against normalized request     │    │                  │
│  │  │ • block / warn / allow                          │    │                  │
│  │  └──────────────────────┬──────────────────────────┘    │                  │
│  │                         │                               │                  │
│  │                         ▼                               │                  │
│  │  ┌─────────────────────────────────────────────────┐    │                  │
│  │  │ Audit Trail                                     │    │                  │
│  │  │ SQLite + hash chain + queue                     │    │                  │
│  │  │ • agent_id, session_id, outcome, provider, model│    │                  │
│  │  │ • openclaw.* metadata when attributed           │    │                  │
│  │  └─────────────────────────────────────────────────┘    │                  │
│  └────────────────────────────────────────────────────────┘                  │
│                 ▲                                                            │
│                 │  provider API calls                                        │
│                 │                                                            │
│  ┌──────────────┴──────────────────────────────────────────────────────────┐ │
│  │       Service-Managed Agents  (run by systemd / launchd)                │ │
│  │                                                                         │ │
│  │  Linux system scope (requires root)                                     │ │
│  │  ┌───────────────────────────────────────────────────────────────────┐   │ │
│  │  │  /etc/systemd/system/openclaw-gateway.service                    │   │ │
│  │  │  + /etc/systemd/system/openclaw-gateway.service.d/               │   │ │
│  │  │      crabwise-proxy.conf   ← created by: crabwise service inject │   │ │
│  │  │      [Service]                                                   │   │ │
│  │  │      Environment="HTTPS_PROXY=http://127.0.0.1:9119"            │   │ │
│  │  │      Environment="NODE_EXTRA_CA_CERTS=~/.../ca.crt"             │   │ │
│  │  └───────────────────────────────────────────────────────────────────┘   │ │
│  │                                                                         │ │
│  │  Linux user scope (no root)                                             │ │
│  │  ┌───────────────────────────────────────────────────────────────────┐   │ │
│  │  │  ~/.config/systemd/user/my-agent.service                         │   │ │
│  │  │  + ~/.config/systemd/user/my-agent.service.d/                    │   │ │
│  │  │      crabwise-proxy.conf   ← created by: crabwise service inject │   │ │
│  │  └───────────────────────────────────────────────────────────────────┘   │ │
│  │                                                                         │ │
│  │  macOS system scope (requires root)                                     │ │
│  │  ┌───────────────────────────────────────────────────────────────────┐   │ │
│  │  │  /Library/LaunchDaemons/com.openclaw.gateway.plist               │   │ │
│  │  │  EnvironmentVariables patched via PlistBuddy                     │   │ │
│  │  │  restart: launchctl kickstart -k system/<label>                  │   │ │
│  │  └───────────────────────────────────────────────────────────────────┘   │ │
│  │                                                                         │ │
│  │  macOS user scope (no root)                                             │ │
│  │  ┌───────────────────────────────────────────────────────────────────┐   │ │
│  │  │  ~/Library/LaunchAgents/com.my-agent.plist                       │   │ │
│  │  │  EnvironmentVariables patched via PlistBuddy                     │   │ │
│  │  │  restart: launchctl kickstart -k gui/<uid>/<label>               │   │ │
│  │  └───────────────────────────────────────────────────────────────────┘   │ │
│  │                                                                         │ │
│  │  Excluded in phase 1: /Library/LaunchAgents (admin-shared agents)       │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                             │
│                 │  allowed requests only                                     │
│                 ▼                                                            │
│           ┌───────────┐                                                     │
│           │ Internet  │  api.openai.com, api.anthropic.com, etc.            │
│           └───────────┘                                                     │
└─────────────────────────────────────────────────────────────────────────────┘

Traffic flow:
  Interactive:       shell → wrap (injects env) → agent → proxy → upstream
  Service (system):  systemd/launchd loads drop-in/plist env → agent → proxy → upstream
  Service (user):    user systemd/launchd loads drop-in/plist env → agent → proxy → upstream

Lifecycle:
  $ sudo crabwise service inject --scope system --service openclaw-gateway
    Linux: writes /etc/systemd/system/openclaw-gateway.service.d/crabwise-proxy.conf
    macOS: patches /Library/LaunchDaemons/com.openclaw.gateway.plist

  $ crabwise service inject --scope user --service my-agent
    Linux: writes ~/.config/systemd/user/my-agent.service.d/crabwise-proxy.conf
    macOS: patches ~/Library/LaunchAgents/com.my-agent.plist

  $ sudo crabwise service remove --scope system --service openclaw-gateway --restart
    Deletes the override, reloads, restarts

Privilege model:
  system scope: requires root (sudo)
  user scope:   no root; sudo --scope user is rejected
```

Scope and privilege model:

- Linux `system`: root, `/etc/systemd/system`, `systemctl`
- Linux `user`: no sudo, `~/.config/systemd/user`, `systemctl --user`
- macOS `system`: root, `/Library/LaunchDaemons`, `launchctl kickstart system/<label>`
- macOS `user`: no sudo, `~/Library/LaunchAgents`, `launchctl kickstart gui/<uid>/<label>`

## CLI Model

The `service` command gets an explicit scope flag:

```bash
crabwise service inject --scope system --service openclaw-gateway
crabwise service inject --scope user --service my-agent
crabwise service remove --scope user --service my-agent
crabwise service status --scope system --service openclaw-gateway
```

Rules:

- `--scope` accepts `system` or `user`
- default is `system`
- no fallback between scopes
- `system` without privileges prints the exact elevated command to run
- `sudo ... --scope user` is rejected instead of trying to operate in root's user domain

## Platform Semantics

### Linux

`system` scope:

- resolve units in system directories such as `/etc/systemd/system`, `/usr/lib/systemd/system`, and `/lib/systemd/system`
- write drop-ins under `/etc/systemd/system/<unit>.service.d/`
- restart with `systemctl daemon-reload` and `systemctl restart <unit>`

`user` scope:

- resolve units in user directories such as `~/.config/systemd/user` and `/etc/systemd/user`
- write drop-ins under `~/.config/systemd/user/<unit>.service.d/`
- restart with `systemctl --user daemon-reload` and `systemctl --user restart <unit>`

### macOS

`system` scope:

- resolve plists in `/Library/LaunchDaemons`
- patch plist `EnvironmentVariables`
- restart with `launchctl kickstart -k system/<label>`

`user` scope:

- resolve plists in `~/Library/LaunchAgents`
- patch plist `EnvironmentVariables`
- restart with `launchctl kickstart -k gui/<uid>/<label>`

Phase 1 intentionally excludes `/Library/LaunchAgents` because it mixes admin-managed files with user-domain execution and complicates privilege handling without a clear product need.

## Architecture

`internal/service` should model a scoped service target explicitly.

Expected shape:

```go
type Scope string

const (
    ScopeSystem Scope = "system"
    ScopeUser   Scope = "user"
)

type Target struct {
    Manager     ServiceManager
    Scope       Scope
    ServiceName string
}

type Resolution struct {
    Target       Target
    UnitName     string
    UnitPath     string
    DropInRoot   string
    PlistPath    string
    LaunchLabel  string
    DomainTarget string
}
```

Commands use one flow:

1. parse `--scope`
2. detect service manager
3. resolve the service in the chosen scope
4. inject, remove, status, and restart against that resolved target

This keeps scope and privilege handling in one place instead of spreading platform rules across CLI helpers.

## Injection Semantics

Injection must match `crabwise wrap` env parity:

- `HTTPS_PROXY`
- `HTTP_PROXY`
- `ALL_PROXY`
- `https_proxy`
- `http_proxy`
- `all_proxy`
- `NO_PROXY`
- `no_proxy`
- `NODE_EXTRA_CA_CERTS` when configured

Systemd uses a dedicated drop-in file. launchd patches `EnvironmentVariables` inside the resolved plist.

Lifecycle examples:

```bash
# Linux/macOS production daemon, default scope
sudo crabwise service inject --scope system --service openclaw-gateway --restart

# User-scoped local daemon
crabwise service inject --scope user --service my-agent --restart

# Verify resolved target and injection state
crabwise service status --scope system --service openclaw-gateway
crabwise service status --scope user --service my-agent

# Remove later
sudo crabwise service remove --scope system --service openclaw-gateway --restart
crabwise service remove --scope user --service my-agent --restart
```

## Status Semantics

Status must distinguish:

- resolution: was the target unit or plist found in the requested scope
- injection: is Crabwise proxy env actually present

Truthfulness rules:

- systemd: `injected` means the Crabwise drop-in file exists and contains a non-empty `HTTPS_PROXY`
- launchd: `injected` means the plist contains a non-empty `EnvironmentVariables:HTTPS_PROXY`

This avoids false positives from orphaned, empty, or corrupted override files.

## Failure Handling

- Missing target in the selected scope returns a hard error for inject and a truthful `not found` status for status
- No automatic fallback from `system` to `user` or vice versa
- `system` scope without privilege prints the exact `sudo crabwise service ...` command
- `user` scope invoked through `sudo` fails with a clear explanation
- Restart failures are reported separately from inject/remove success

## Testing Strategy

- pure resolution tests for scope-aware path selection
- env parity tests proving `service` injection matches `wrap`
- temp-dir tests for scoped systemd drop-in write/remove/status behavior
- launchd tests for scope resolution, label reading, domain targeting, and injected-state detection
- CLI tests for:
  - default `--scope system`
  - explicit `--scope user`
  - privilege messaging
  - truthful status output

## Recommendation

Implement this as a scoped platform feature now rather than layering user-service support on later. The extra design work is justified because it gives Crabwise one coherent governance story for daemonized agents:

- interactive agents use `crabwise wrap`
- daemon agents use `crabwise service --scope <system|user>`

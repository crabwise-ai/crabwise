# Service Inject Design

Date: 2026-02-28
Status: approved for planning

## Summary

This design turns `crabwise service` into a general platform feature for governing daemon-managed agents across both system and user service domains.

The CLI keeps `system` as the default scope because OpenClaw and most production daemon installs live there, but the feature must support `user` scope equally well on both Linux and macOS.

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

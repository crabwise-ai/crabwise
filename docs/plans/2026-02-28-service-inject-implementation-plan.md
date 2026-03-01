# Service Inject Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `crabwise service inject|remove|status` as a scoped platform feature that governs daemon-managed agents across both `system` and `user` service domains on Linux and macOS.

**Architecture:** Add a new `internal/service` package that models a scoped service target, resolves it per platform, and performs inject/remove/status/restart operations against that resolved target. The CLI exposes `--scope system|user` with default `system`, keeps scope handling explicit, and uses truthful status checks based on actual injected env keys rather than path existence alone.

**Tech Stack:** Go, Cobra, runtime.GOOS, os/exec, systemd drop-ins, launchd plist mutation via PlistBuddy

---

### Task 1: Add scope-aware service model and env parity helpers

**Files:**
- Create: `internal/service/types.go`
- Create: `internal/service/env.go`
- Test: `internal/service/env_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestParseScope_DefaultSystem`
- `TestParseScope_User`
- `TestParseScope_Invalid`
- `TestProxyEnvVars_MatchesWrapParity`

Suggested test shape:

```go
func TestProxyEnvVars_MatchesWrapParity(t *testing.T) {
	got := proxyEnvVars(EnvConfig{
		ProxyURL: "http://127.0.0.1:9119",
		CACert:   "/tmp/ca.crt",
	})

	keys := make([]string, 0, len(got))
	for _, env := range got {
		keys = append(keys, env.Key)
	}

	require.ElementsMatch(t, []string{
		"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY",
		"https_proxy", "http_proxy", "all_proxy",
		"NO_PROXY", "no_proxy", "NODE_EXTRA_CA_CERTS",
	}, keys)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service -run 'TestParseScope_|TestProxyEnvVars_MatchesWrapParity' -v
```

Expected: FAIL because the package/types do not exist yet.

**Step 3: Write minimal implementation**

Create `internal/service/types.go`:

```go
package service

import "fmt"

type ServiceManager string
type Scope string

const (
	ManagerSystemd ServiceManager = "systemd"
	ManagerLaunchd ServiceManager = "launchd"
	ManagerUnknown ServiceManager = "unknown"
)

const (
	ScopeSystem Scope = "system"
	ScopeUser   Scope = "user"
)

type Target struct {
	Manager     ServiceManager
	Scope       Scope
	ServiceName string
}

type EnvConfig struct {
	ProxyURL string
	CACert   string
}

type EnvVar struct {
	Key   string
	Value string
}

func ParseScope(raw string) (Scope, error) {
	switch raw {
	case "", string(ScopeSystem):
		return ScopeSystem, nil
	case string(ScopeUser):
		return ScopeUser, nil
	default:
		return "", fmt.Errorf("invalid scope %q; expected system or user", raw)
	}
}
```

Create `internal/service/env.go`:

```go
package service

func proxyEnvVars(cfg EnvConfig) []EnvVar {
	vars := []EnvVar{
		{Key: "HTTPS_PROXY", Value: cfg.ProxyURL},
		{Key: "HTTP_PROXY", Value: cfg.ProxyURL},
		{Key: "ALL_PROXY", Value: cfg.ProxyURL},
		{Key: "https_proxy", Value: cfg.ProxyURL},
		{Key: "http_proxy", Value: cfg.ProxyURL},
		{Key: "all_proxy", Value: cfg.ProxyURL},
		{Key: "NO_PROXY", Value: "localhost,127.0.0.1"},
		{Key: "no_proxy", Value: "localhost,127.0.0.1"},
	}
	if cfg.CACert != "" {
		vars = append(vars, EnvVar{Key: "NODE_EXTRA_CA_CERTS", Value: cfg.CACert})
	}
	return vars
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/service -run 'TestParseScope_|TestProxyEnvVars_MatchesWrapParity' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/types.go internal/service/env.go internal/service/env_test.go
git commit -m "feat: add scoped service model and env parity helpers"
```

---

### Task 2: Add scope-aware resolution for systemd and launchd

**Files:**
- Create: `internal/service/resolve.go`
- Test: `internal/service/resolve_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestDetectServiceManager`
- `TestResolveSystemdTarget_SystemScope`
- `TestResolveSystemdTarget_UserScope`
- `TestResolveLaunchdTarget_SystemScope`
- `TestResolveLaunchdTarget_UserScope`
- `TestResolveTarget_DoesNotFallbackAcrossScopes`

Make the resolver injectable with test directory lists instead of hardcoding OS paths in tests.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service -run 'TestDetectServiceManager|TestResolve(Systemd|Launchd)Target_|TestResolveTarget_DoesNotFallbackAcrossScopes' -v
```

Expected: FAIL because resolution does not exist yet.

**Step 3: Write minimal implementation**

Create `internal/service/resolve.go` with:

- `DetectServiceManager()`
- `type Resolution struct { ... }`
- injected directory lists for systemd system, systemd user, launchd system, launchd user
- `ResolveTarget(target Target) (Resolution, error)`

Systemd rules:

- `system` scope searches `/etc/systemd/system`, `/usr/lib/systemd/system`, `/lib/systemd/system`
- `user` scope searches `~/.config/systemd/user`, `/etc/systemd/user`
- drop-in root is `/etc/systemd/system` for system scope
- drop-in root is `~/.config/systemd/user` for user scope

launchd rules:

- `system` scope searches `/Library/LaunchDaemons`
- `user` scope searches `~/Library/LaunchAgents`
- resolution returns plist path only; label is read later

**Step 4: Run test to verify it passes**

```bash
go test ./internal/service -run 'TestDetectServiceManager|TestResolve(Systemd|Launchd)Target_|TestResolveTarget_DoesNotFallbackAcrossScopes' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/resolve.go internal/service/resolve_test.go
git commit -m "feat: add scoped service resolution"
```

---

### Task 3: Add platform-specific inject/remove/status helpers with truthful checks

**Files:**
- Create: `internal/service/systemd.go`
- Create: `internal/service/launchd.go`
- Test: `internal/service/systemd_test.go`
- Test: `internal/service/launchd_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestGenerateSystemdDropIn_UsesWrapParity`
- `TestInjectSystemd_WritesScopedDropIn`
- `TestRemoveSystemd_RemovesScopedDropIn`
- `TestCheckSystemdInjected_RequiresHTTPSProxyInDropIn`
- `TestGenerateLaunchdCommands_UsesWrapParity`
- `TestCheckLaunchdInjected_UsesPlistBuddyHTTPSProxy`
- `TestReadLaunchdLabel`
- `TestLaunchdDomain_SystemScope`
- `TestLaunchdDomain_UserScope`

For systemd injected-state tests, verify that:

- existing drop-in with `HTTPS_PROXY` => injected
- existing empty or corrupted drop-in => not injected

For launchd, wrap exec calls behind overridable package vars so tests can stub PlistBuddy output.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service -run 'Test(GenerateSystemdDropIn|InjectSystemd|RemoveSystemd|CheckSystemdInjected|GenerateLaunchdCommands|CheckLaunchdInjected|ReadLaunchdLabel|LaunchdDomain)' -v
```

Expected: FAIL because helpers do not exist yet.

**Step 3: Write minimal implementation**

Create `internal/service/systemd.go`:

- `GenerateSystemdDropIn(cfg EnvConfig) string`
- `InjectSystemd(res Resolution, cfg EnvConfig) (InjectResult, error)`
- `RemoveSystemd(res Resolution) (RemoveResult, error)`
- `CheckSystemdInjected(res Resolution) bool`

`CheckSystemdInjected` must open the Crabwise drop-in and require a non-empty `HTTPS_PROXY=` entry.

Create `internal/service/launchd.go`:

- `GenerateLaunchdPlistCommands(plistPath string, cfg EnvConfig) []string`
- `GenerateLaunchdRemoveCommands(plistPath string, cfg EnvConfig) []string`
- `CheckLaunchdInjected(plistPath string) bool`
- `ReadLaunchdLabel(plistPath string) (string, error)`
- `LaunchdDomain(scope Scope, uid int) string`

Use package-level exec shims:

```go
var plistBuddyOutput = func(args ...string) ([]byte, error) { ... }
var plistBuddyRun = func(cmd string) error { ... }
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/service -run 'Test(GenerateSystemdDropIn|InjectSystemd|RemoveSystemd|CheckSystemdInjected|GenerateLaunchdCommands|CheckLaunchdInjected|ReadLaunchdLabel|LaunchdDomain)' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/systemd.go internal/service/launchd.go internal/service/systemd_test.go internal/service/launchd_test.go
git commit -m "feat: add scoped service platform operations"
```

---

### Task 4: Add restart helpers and privilege/domain guards

**Files:**
- Create: `internal/service/restart.go`
- Test: `internal/service/restart_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestSystemdRestart_SystemScopeCommand`
- `TestSystemdRestart_UserScopeCommand`
- `TestLaunchdRestart_SystemScopeCommand`
- `TestLaunchdRestart_UserScopeCommand`
- `TestValidatePrivileges_SystemRequiresRoot`
- `TestValidatePrivileges_UserRejectsSudo`

Use overridable exec-command shims so tests verify the intended command argv without calling real `systemctl` or `launchctl`.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service -run 'Test(SystemdRestart|LaunchdRestart|ValidatePrivileges)' -v
```

Expected: FAIL because restart and privilege helpers do not exist.

**Step 3: Write minimal implementation**

Create `internal/service/restart.go` with:

- `ValidatePrivileges(scope Scope, uid int, sudoUser string) error`
- `RestartTarget(res Resolution) error`

Rules:

- `system` scope requires elevated privileges
- `user` scope rejects `sudo`/uid mismatch rather than operating in root's user domain
- Linux user restart uses `systemctl --user`
- macOS user restart uses `launchctl kickstart -k gui/<uid>/<label>`

**Step 4: Run test to verify it passes**

```bash
go test ./internal/service -run 'Test(SystemdRestart|LaunchdRestart|ValidatePrivileges)' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/restart.go internal/service/restart_test.go
git commit -m "feat: add scoped service restart and privilege checks"
```

---

### Task 5: Add `crabwise service` CLI with `--scope`

**Files:**
- Create: `internal/cli/service.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/service_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestServiceCommand_DefaultScopeIsSystem`
- `TestServiceInjectCmd_DryRun_SystemScope`
- `TestServiceInjectCmd_DryRun_UserScope`
- `TestServiceStatusCmd_NotFound`
- `TestServiceStatusCmd_NotInjected`
- `TestServiceRejectsSudoForUserScope`
- `TestEnvConfigFromDaemonConfig`

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cli -run 'TestService(Command_|InjectCmd_|StatusCmd_|RejectsSudo)|TestEnvConfigFromDaemonConfig' -v
```

Expected: FAIL because the command does not exist yet.

**Step 3: Write minimal implementation**

Create `internal/cli/service.go` with:

- root `service` command
- `inject`, `remove`, `status` subcommands
- `--scope` string flag defaulting to `system`
- `--service`, `--config`, and `--restart`
- dry-run support for both scopes

Implementation flow:

1. load daemon config
2. parse scope with `service.ParseScope`
3. detect manager
4. resolve target with `service.ResolveTarget`
5. validate privileges
6. inject/remove/status/restart through `internal/service`

User messaging rules:

- `system` without privilege prints the exact `sudo crabwise service ... --scope system ...` command
- `user` under sudo returns a hard error explaining that user scope must be run as the owning user
- `status` prints both resolution and injection state when useful

Add `newServiceCmd()` to the root command tree in `internal/cli/root.go`.

**Step 4: Run test to verify it passes**

```bash
go test ./internal/cli -run 'TestService(Command_|InjectCmd_|StatusCmd_|RejectsSudo)|TestEnvConfigFromDaemonConfig' -v
```

Expected: PASS

**Step 5: Verify the command tree**

```bash
go build -o /dev/null ./cmd/crabwise
go run ./cmd/crabwise service --help
go run ./cmd/crabwise service inject --help
go run ./cmd/crabwise service remove --help
go run ./cmd/crabwise service status --help
```

Expected: all commands compile and print help text without errors.

**Step 6: Commit**

```bash
git add internal/cli/service.go internal/cli/root.go internal/cli/service_test.go
git commit -m "feat: add scoped crabwise service commands"
```

---

### Task 6: Update docs for system and user service domains

**Files:**
- Modify: `README.md`
- Modify: `docs/plans/proxy_enforcement-howto.md`

**Step 1: Write the failing docs check**

```bash
rg -n -- '--scope|systemctl --user|~/Library/LaunchAgents|crabwise service inject' README.md docs/plans/proxy_enforcement-howto.md
```

Expected before edits: missing or incomplete scope coverage.

**Step 2: Write minimal documentation**

In `README.md`, add a `### crabwise service` section that documents:

- default `--scope system`
- `--scope user`
- Linux systemd system vs user behavior
- macOS LaunchDaemons vs `~/Library/LaunchAgents`
- `system = sudo`, `user = no sudo`

Include examples:

```bash
sudo crabwise service inject --scope system --service openclaw-gateway --restart
crabwise service inject --scope user --service my-agent --restart
crabwise service status --scope system --service openclaw-gateway
crabwise service status --scope user --service my-agent
```

In `docs/plans/proxy_enforcement-howto.md`, update the OpenClaw production example to call out:

- OpenClaw standard production install uses `--scope system`
- user-scoped agents should use `--scope user` and not `sudo`

**Step 3: Verify documentation**

```bash
rg -n -- '--scope|systemctl --user|~/Library/LaunchAgents|crabwise service inject' README.md docs/plans/proxy_enforcement-howto.md
```

Expected: both files contain scoped examples and platform notes.

**Step 4: Commit**

```bash
git add README.md docs/plans/proxy_enforcement-howto.md
git commit -m "docs: add scoped service inject guide"
```

---

### Task 7: Final verification pass

**Files:**
- No code changes required unless failures appear

**Step 1: Run focused package tests**

```bash
go test ./internal/service ./internal/cli -v
```

Expected: PASS

**Step 2: Run broader regression**

```bash
go test ./...
```

Expected: PASS

**Step 3: Run linter**

```bash
golangci-lint run ./internal/service/... ./internal/cli/...
```

Expected: no new warnings.

**Step 4: Inspect git state**

```bash
git status --short
git log --oneline -n 10
```

Expected: only intended files changed and commits remain granular.

**Step 5: Commit any final fixups**

```bash
git add -A
git commit -m "chore: finalize scoped service inject feature"
```

Only do this if verification required cleanup changes.

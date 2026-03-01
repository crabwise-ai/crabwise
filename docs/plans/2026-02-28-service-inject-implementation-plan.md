# Service Inject Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `crabwise service inject|remove|status` as a scoped platform feature that governs daemon-managed agents across both `system` and `user` service domains on Linux and macOS.

**Architecture:** Add a new `internal/service` package that models a scoped service target, resolves it per platform, and performs inject/remove/status/restart operations against that resolved target. The CLI exposes `--scope system|user` with default `system`, keeps scope handling explicit, and uses truthful status checks based on actual injected env keys rather than path existence alone.

**Tech Stack:** Go, Cobra, runtime.GOOS, os/exec, systemd drop-ins, launchd plist mutation via PlistBuddy

---

## Problem Statement

Crabwise already governs provider calls for interactive agents by injecting proxy environment variables at process launch time through `crabwise wrap`.

That breaks down for service-managed agents because:

1. they are started by system managers rather than an interactive shell
2. they persist across restarts and reboots
3. the proxy environment must live in the service definition, not just the shell session

This feature closes that gap for both service domains that matter operationally:

- `system` services used by production daemons like OpenClaw Gateway
- `user` services used by local per-user agents and developer daemons

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
│  │  │ Proxy :9119   │  │ (read-only observer)            │ │                  │
│  │  │               │  │ • session cache                 │ │                  │
│  │  │ • intercept   │  │ • run correlation               │ │                  │
│  │  │ • decrypt     │  │ • event enrichment              │ │                  │
│  │  │ • evaluate    │  └────────────┬────────────────────┘ │                  │
│  │  │ • block/allow │              │                      │                  │
│  │  └──────┬───────┘  ┌────────────┴───────────────────┐  │                  │
│  │         │          │ Correlation Store (in-memory)   │  │                  │
│  │         │          │ runId → sessionKey → agentId    │  │                  │
│  │         │          └────────────────────────────────┘   │                  │
│  │         │                  ▲ lookup                     │                  │
│  │         ▼                  │                            │                  │
│  │  ┌─────────────────────────┴───────────────────────┐    │                  │
│  │  │ Commandment Engine → Audit Trail (SQLite)       │    │                  │
│  │  │ block / warn / allow → hash-chain → queue       │    │                  │
│  │  └─────────────────────────────────────────────────┘    │                  │
│  └────────────────────────────────────────────────────────┘                  │
│                 ▲                                                            │
│                 │  provider API calls                                        │
│                 │                                                            │
│  ┌──────────────┴──────────────────────────────────────────────────────────┐ │
│  │       Service-Managed Agents  (run by systemd / launchd)                │ │
│  │                                                                         │ │
│  │  Linux system scope (root)          Linux user scope (no root)           │ │
│  │  ┌──────────────────────────────┐   ┌──────────────────────────────┐     │ │
│  │  │ /etc/systemd/system/         │   │ ~/.config/systemd/user/      │     │ │
│  │  │   <unit>.service.d/          │   │   <unit>.service.d/          │     │ │
│  │  │   crabwise-proxy.conf       │   │   crabwise-proxy.conf       │     │ │
│  │  └──────────────────────────────┘   └──────────────────────────────┘     │ │
│  │                                                                         │ │
│  │  macOS system scope (root)          macOS user scope (no root)           │ │
│  │  ┌──────────────────────────────┐   ┌──────────────────────────────┐     │ │
│  │  │ /Library/LaunchDaemons/      │   │ ~/Library/LaunchAgents/      │     │ │
│  │  │   <label>.plist              │   │   <label>.plist              │     │ │
│  │  │   EnvironmentVariables patch │   │   EnvironmentVariables patch │     │ │
│  │  └──────────────────────────────┘   └──────────────────────────────┘     │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                             │
│                 │  allowed requests only                                     │
│                 ▼                                                            │
│           ┌───────────┐                                                     │
│           │ Internet  │  api.openai.com, api.anthropic.com, etc.            │
│           └───────────┘                                                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

Scope rules this plan must preserve:

- default scope is `system`
- no fallback between scopes
- `system` is privileged
- `user` is unprivileged
- injected status must mean `HTTPS_PROXY` is actually present

Lifecycle examples this implementation must support:

```bash
sudo crabwise service inject --scope system --service openclaw-gateway --restart
crabwise service inject --scope user --service my-agent --restart
crabwise service status --scope system --service openclaw-gateway
crabwise service status --scope user --service my-agent
```

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

```go
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const dropInFileName = "crabwise-proxy.conf"

type InjectResult struct {
	Path    string
	Written bool
}

type RemoveResult struct {
	Path    string
	Removed bool
}

func GenerateSystemdDropIn(cfg EnvConfig) string {
	var b strings.Builder
	b.WriteString("# Generated by crabwise service inject — do not edit manually.\n")
	b.WriteString("[Service]\n")
	for _, env := range proxyEnvVars(cfg) {
		b.WriteString(fmt.Sprintf("Environment=\"%s=%s\"\n", env.Key, env.Value))
	}
	return b.String()
}

func InjectSystemd(res Resolution, cfg EnvConfig) (InjectResult, error) {
	dropInDir := filepath.Join(res.DropInRoot, res.UnitName+".d")
	dropInPath := filepath.Join(dropInDir, dropInFileName)

	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		return InjectResult{Path: dropInPath}, fmt.Errorf("create drop-in dir: %w", err)
	}

	content := GenerateSystemdDropIn(cfg)
	if err := os.WriteFile(dropInPath, []byte(content), 0644); err != nil {
		return InjectResult{Path: dropInPath}, fmt.Errorf("write drop-in: %w", err)
	}

	return InjectResult{Path: dropInPath, Written: true}, nil
}

func RemoveSystemd(res Resolution) (RemoveResult, error) {
	dropInDir := filepath.Join(res.DropInRoot, res.UnitName+".d")
	dropInPath := filepath.Join(dropInDir, dropInFileName)

	if _, err := os.Stat(dropInPath); os.IsNotExist(err) {
		return RemoveResult{Path: dropInPath, Removed: false}, nil
	}

	if err := os.Remove(dropInPath); err != nil {
		return RemoveResult{Path: dropInPath}, fmt.Errorf("remove drop-in: %w", err)
	}

	entries, err := os.ReadDir(dropInDir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dropInDir)
	}

	return RemoveResult{Path: dropInPath, Removed: true}, nil
}

// CheckSystemdInjected returns true only if the drop-in file exists AND
// contains a non-empty HTTPS_PROXY assignment. Path existence alone is
// not sufficient — a corrupted or emptied file must not report as injected.
func CheckSystemdInjected(res Resolution) bool {
	dropInPath := filepath.Join(res.DropInRoot, res.UnitName+".d", dropInFileName)
	data, err := os.ReadFile(dropInPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "HTTPS_PROXY=") && !strings.HasSuffix(strings.TrimSpace(line), "HTTPS_PROXY=\"\"") {
			return true
		}
	}
	return false
}
```

Create `internal/service/launchd.go`:

```go
package service

import (
	"fmt"
	"os/exec"
	"strings"
)

// Exec shims for testing. Tests override these to avoid calling real PlistBuddy.
var execCommand = exec.Command
var execOutput = func(name string, args ...string) ([]byte, error) {
	return execCommand(name, args...).Output()
}

func GenerateLaunchdPlistCommands(plistPath string, cfg EnvConfig) []string {
	envVars := proxyEnvVars(cfg)
	cmds := []string{
		fmt.Sprintf("/usr/libexec/PlistBuddy -c 'Add :EnvironmentVariables dict' %s 2>/dev/null || true", plistPath),
	}
	for _, env := range envVars {
		cmds = append(cmds, fmt.Sprintf(
			"/usr/libexec/PlistBuddy -c 'Set :EnvironmentVariables:%s %s' %s 2>/dev/null || "+
				"/usr/libexec/PlistBuddy -c 'Add :EnvironmentVariables:%s string %s' %s",
			env.Key, env.Value, plistPath,
			env.Key, env.Value, plistPath,
		))
	}
	return cmds
}

func GenerateLaunchdRemoveCommands(plistPath string, cfg EnvConfig) []string {
	envVars := proxyEnvVars(cfg)
	var cmds []string
	for _, env := range envVars {
		cmds = append(cmds, fmt.Sprintf(
			"/usr/libexec/PlistBuddy -c 'Delete :EnvironmentVariables:%s' %s 2>/dev/null || true",
			env.Key, plistPath,
		))
	}
	return cmds
}

// CheckLaunchdInjected returns true only if the plist contains a non-empty
// HTTPS_PROXY in EnvironmentVariables. This avoids false positives from
// plists that exist but were never injected.
func CheckLaunchdInjected(plistPath string) bool {
	out, err := execOutput(
		"/usr/libexec/PlistBuddy", "-c", "Print :EnvironmentVariables:HTTPS_PROXY", plistPath,
	)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// ReadLaunchdLabel reads the Label key from a plist. The label is the
// canonical launchd service identifier and may differ from the filename.
func ReadLaunchdLabel(plistPath string) (string, error) {
	out, err := execOutput(
		"/usr/libexec/PlistBuddy", "-c", "Print :Label", plistPath,
	)
	if err != nil {
		return "", fmt.Errorf("read Label from %s: %w", plistPath, err)
	}
	label := strings.TrimSpace(string(out))
	if label == "" {
		return "", fmt.Errorf("empty Label in %s", plistPath)
	}
	return label, nil
}

// LaunchdDomain returns the launchctl domain target for the given scope.
// system scope uses "system/<label>", user scope uses "gui/<uid>/<label>".
func LaunchdDomain(scope Scope, uid int, label string) string {
	if scope == ScopeSystem {
		return "system/" + label
	}
	return fmt.Sprintf("gui/%d/%s", uid, label)
}
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

Create `internal/service/restart.go`:

```go
package service

import (
	"fmt"
	"os"
	"os/exec"
)

// ValidatePrivileges checks whether the current process has the right
// privilege level for the requested scope. Returns a descriptive error
// when the privilege model is violated.
//
// Rules:
//   - system scope requires uid 0 (root)
//   - user scope rejects uid 0 when SUDO_USER is set, because that
//     means the user ran "sudo crabwise service --scope user ..." which
//     would operate in root's user domain instead of their own
func ValidatePrivileges(scope Scope, uid int, sudoUser string) error {
	switch scope {
	case ScopeSystem:
		if uid != 0 {
			return fmt.Errorf("system scope requires root; run with sudo")
		}
	case ScopeUser:
		if uid == 0 && sudoUser != "" {
			return fmt.Errorf(
				"user scope must be run as the owning user, not through sudo; "+
					"run without sudo as %q instead", sudoUser,
			)
		}
	}
	return nil
}

// SuggestElevatedCommand returns the sudo-prefixed version of the
// current command for printing when system scope fails the privilege check.
func SuggestElevatedCommand(args []string) string {
	return "sudo " + joinArgs(args)
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// restartCmd is overridable for testing.
var restartCmd = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RestartTarget reloads and restarts the service described by the resolution.
func RestartTarget(res Resolution) error {
	switch res.Target.Manager {
	case ManagerSystemd:
		return restartSystemd(res)
	case ManagerLaunchd:
		return restartLaunchd(res)
	default:
		return fmt.Errorf("unsupported manager %q", res.Target.Manager)
	}
}

func restartSystemd(res Resolution) error {
	if res.Target.Scope == ScopeUser {
		if err := restartCmd("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl --user daemon-reload: %w", err)
		}
		return restartCmd("systemctl", "--user", "restart", res.Target.ServiceName)
	}
	if err := restartCmd("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return restartCmd("systemctl", "restart", res.Target.ServiceName)
}

func restartLaunchd(res Resolution) error {
	if res.DomainTarget == "" {
		return fmt.Errorf("no domain target resolved for launchd restart")
	}
	return restartCmd("launchctl", "kickstart", "-k", res.DomainTarget)
}
```

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

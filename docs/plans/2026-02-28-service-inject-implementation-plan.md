# Service Inject Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `crabwise service inject|remove|status` as a scoped platform feature that governs daemon-managed agents across both `system` and `user` service domains on Linux and macOS.

**Architecture:** Add a new `internal/service` package that defines a `Manager` interface implemented by `SystemdManager` and `LaunchdManager`. Resolution uses embedded platform-specific structs so consumers never read fields that don't apply. Proxy env vars are defined once and shared by both `crabwise wrap` and `crabwise service inject`. The CLI exposes `--scope system|user` with default `system`.

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
sudo crabwise service inject --agent openclaw --restart
crabwise service inject --scope user --agent my-agent --restart
crabwise service status --agent openclaw
crabwise service status --scope user --agent my-agent
```

---

### Task 1: Add shared types, Manager interface, and env parity helpers

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
- `TestProxyEnvVars_NoCACert`
- `TestResolveAgentName_KnownLinux`
- `TestResolveAgentName_KnownDarwin`
- `TestResolveAgentName_UnknownFallback`

Suggested test shape:

```go
package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseScope_DefaultSystem(t *testing.T) {
	scope, err := ParseScope("")
	require.NoError(t, err)
	require.Equal(t, ScopeSystem, scope)
}

func TestParseScope_User(t *testing.T) {
	scope, err := ParseScope("user")
	require.NoError(t, err)
	require.Equal(t, ScopeUser, scope)
}

func TestParseScope_Invalid(t *testing.T) {
	_, err := ParseScope("global")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid scope")
}

func TestProxyEnvVars_MatchesWrapParity(t *testing.T) {
	got := ProxyEnvVars(EnvConfig{
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

func TestProxyEnvVars_NoCACert(t *testing.T) {
	got := ProxyEnvVars(EnvConfig{
		ProxyURL: "http://127.0.0.1:9119",
	})

	for _, env := range got {
		require.NotEqual(t, "NODE_EXTRA_CA_CERTS", env.Key)
	}
}

func TestResolveAgentName_KnownLinux(t *testing.T) {
	agents := map[string]AgentServiceEntry{
		"openclaw": {SystemdUnit: "openclaw-gateway", LaunchdPlist: "com.openclaw.gateway"},
	}
	require.Equal(t, "openclaw-gateway", ResolveAgentName("openclaw", agents, "linux"))
}

func TestResolveAgentName_KnownDarwin(t *testing.T) {
	agents := map[string]AgentServiceEntry{
		"openclaw": {SystemdUnit: "openclaw-gateway", LaunchdPlist: "com.openclaw.gateway"},
	}
	require.Equal(t, "com.openclaw.gateway", ResolveAgentName("openclaw", agents, "darwin"))
}

func TestResolveAgentName_UnknownFallback(t *testing.T) {
	agents := map[string]AgentServiceEntry{
		"openclaw": {SystemdUnit: "openclaw-gateway", LaunchdPlist: "com.openclaw.gateway"},
	}
	require.Equal(t, "my-custom-daemon", ResolveAgentName("my-custom-daemon", agents, "linux"))
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service -run 'TestParseScope_|TestProxyEnvVars_|TestResolveAgentName_' -v
```

Expected: FAIL because the package does not exist yet.

**Step 3: Write minimal implementation**

Create `internal/service/types.go`:

```go
package service

import "fmt"

// Scope identifies whether a service target lives in the system or user domain.
type Scope string

const (
	ScopeSystem Scope = "system"
	ScopeUser   Scope = "user"
)

// EnvConfig holds the values needed to generate proxy environment variables.
type EnvConfig struct {
	ProxyURL string
	CACert   string
}

// EnvVar is a key-value pair for an environment variable.
type EnvVar struct {
	Key   string
	Value string
}

// InjectResult describes the outcome of an inject operation.
type InjectResult struct {
	Path    string
	Written bool
}

// RemoveResult describes the outcome of a remove operation.
type RemoveResult struct {
	Path    string
	Removed bool
}

// Resolution describes a resolved service target. Exactly one of
// Systemd or Launchd is non-nil depending on the detected platform.
type Resolution struct {
	ServiceName string
	Scope       Scope
	Systemd     *SystemdResolution
	Launchd     *LaunchdResolution
}

// SystemdResolution holds systemd-specific resolution details.
type SystemdResolution struct {
	UnitName   string
	UnitPath   string
	DropInRoot string
}

// LaunchdResolution holds launchd-specific resolution details.
type LaunchdResolution struct {
	PlistPath    string
	Label        string
	DomainTarget string
}

// Manager is the interface that platform-specific service managers implement.
// Each method operates on a Resolution returned by Resolve.
type Manager interface {
	Resolve(name string, scope Scope) (Resolution, error)
	Inject(res Resolution, cfg EnvConfig) (InjectResult, error)
	Remove(res Resolution, cfg EnvConfig) (RemoveResult, error)
	CheckInjected(res Resolution) bool
	Restart(res Resolution) error
}

// AgentServiceEntry maps a friendly agent name to platform-specific service names.
type AgentServiceEntry struct {
	SystemdUnit  string `yaml:"systemd_unit"`
	LaunchdPlist string `yaml:"launchd_plist"`
}

// ResolveAgentName looks up a friendly agent name in the registry and returns
// the platform-specific service name. If the agent is not in the registry,
// the name is returned as-is (literal fallback).
func ResolveAgentName(agent string, agents map[string]AgentServiceEntry, goos string) string {
	entry, ok := agents[agent]
	if !ok {
		return agent
	}
	switch goos {
	case "linux":
		if entry.SystemdUnit != "" {
			return entry.SystemdUnit
		}
	case "darwin":
		if entry.LaunchdPlist != "" {
			return entry.LaunchdPlist
		}
	}
	return agent
}

// ParseScope parses a raw scope string. Empty string defaults to system.
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

// ProxyEnvVars returns the canonical set of proxy environment variables.
// This is the single source of truth used by both `crabwise wrap` and
// `crabwise service inject`.
func ProxyEnvVars(cfg EnvConfig) []EnvVar {
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
go test ./internal/service -run 'TestParseScope_|TestProxyEnvVars_|TestResolveAgentName_' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/types.go internal/service/env.go internal/service/env_test.go
git commit -m "feat: add service Manager interface and shared proxy env helpers"
```

---

### Task 2: Refactor `crabwise wrap` to use shared proxy env

**Files:**
- Modify: `internal/cli/wrap.go`

**Step 1: Run existing wrap tests to confirm baseline**

```bash
go test ./internal/cli -run 'TestWrap|TestOverlayEnv|TestProxyEnv' -v
```

Expected: PASS (baseline before refactor).

**Step 2: Refactor wrap.go**

Replace the private `envPair` type and `proxyEnvPairs` function with the shared `service.ProxyEnvVars` and `service.EnvVar`. Add a shared `envConfigFromDaemon` helper that both `wrap` and `service` CLI commands will use.

After refactor, `wrap.go` should look like:

```go
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/service"
	"github.com/spf13/cobra"
)

// envConfigFromDaemon constructs a service.EnvConfig from the daemon config.
// Used by both wrap and service inject commands.
func envConfigFromDaemon(cfg *daemon.Config) service.EnvConfig {
	return service.EnvConfig{
		ProxyURL: "http://" + cfg.Adapters.Proxy.Listen,
		CACert:   cfg.Adapters.Proxy.CACert,
	}
}

func overlayEnv(base []string, vars []service.EnvVar) []string {
	overrides := make(map[string]string, len(vars))
	for _, v := range vars {
		overrides[v.Key] = v.Value
	}

	var result []string
	seen := make(map[string]bool)
	for _, entry := range base {
		k, _, _ := strings.Cut(entry, "=")
		if v, ok := overrides[k]; ok {
			result = append(result, k+"="+v)
			seen[k] = true
		} else {
			result = append(result, entry)
		}
	}
	for _, v := range vars {
		if !seen[v.Key] {
			result = append(result, v.Key+"="+v.Value)
		}
	}
	return result
}

func newWrapCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "wrap -- <command> [args...]",
		Short: "Run a command with proxy environment configured",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("no command specified; usage: crabwise wrap -- <command> [args...]")
			}

			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			binary, err := exec.LookPath(args[0])
			if err != nil {
				return fmt.Errorf("resolve command %q: %w", args[0], err)
			}

			env := overlayEnv(os.Environ(), service.ProxyEnvVars(envConfigFromDaemon(cfg)))
			return syscall.Exec(binary, args, env)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}
```

Key changes:

- Remove `envPair` type (replaced by `service.EnvVar`)
- Remove `proxyEnvPairs` function (replaced by `service.ProxyEnvVars`)
- Add `envConfigFromDaemon` helper (will be reused by service CLI in Task 6)
- `overlayEnv` now takes `[]service.EnvVar` instead of `[]envPair`

**Step 3: Run tests to verify refactor is clean**

```bash
go test ./internal/cli -run 'TestWrap|TestOverlayEnv|TestProxyEnv' -v
go build -o /dev/null ./cmd/crabwise
```

Expected: PASS, clean build.

**Step 4: Commit**

```bash
git add internal/cli/wrap.go
git commit -m "refactor: wrap uses shared proxy env from internal/service"
```

---

### Task 3: Implement SystemdManager

**Files:**
- Create: `internal/service/systemd.go`
- Test: `internal/service/systemd_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestSystemdManager_Resolve_SystemScope`
- `TestSystemdManager_Resolve_UserScope`
- `TestSystemdManager_Resolve_NotFound`
- `TestSystemdManager_Resolve_NoFallbackAcrossScopes`
- `TestSystemdManager_Inject_WritesDropIn`
- `TestSystemdManager_Inject_Idempotent`
- `TestSystemdManager_Remove_DeletesDropIn`
- `TestSystemdManager_Remove_CleansEmptyDir`
- `TestSystemdManager_Remove_NotInjected`
- `TestSystemdManager_CheckInjected_ValidDropIn`
- `TestSystemdManager_CheckInjected_EmptyFile`
- `TestSystemdManager_CheckInjected_MissingHeader`
- `TestSystemdManager_CheckInjected_NoFile`
- `TestSystemdManager_Restart_SystemScope`
- `TestSystemdManager_Restart_UserScope`

All tests use temp directories injected into `SystemDirs`/`UserDirs` fields. Restart tests override `RunCmd` to capture invoked commands.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service -run 'TestSystemdManager_' -v
```

Expected: FAIL because the implementation does not exist.

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
const dropInHeader = "# Generated by crabwise service inject — do not edit manually.\n"

// SystemdManager implements Manager for systemd-based systems.
type SystemdManager struct {
	SystemDirs []string
	UserDirs   []string
	RunCmd     func(name string, args ...string) error
}

// NewSystemdManager returns a SystemdManager with production defaults.
func NewSystemdManager() *SystemdManager {
	home, _ := os.UserHomeDir()
	return &SystemdManager{
		SystemDirs: []string{
			"/etc/systemd/system",
			"/usr/lib/systemd/system",
			"/lib/systemd/system",
		},
		UserDirs: []string{
			filepath.Join(home, ".config", "systemd", "user"),
			"/etc/systemd/user",
		},
		RunCmd: defaultRunCmd,
	}
}

func (m *SystemdManager) Resolve(name string, scope Scope) (Resolution, error) {
	unitName := name + ".service"
	dirs := m.SystemDirs
	dropInRoot := "/etc/systemd/system"

	if scope == ScopeUser {
		dirs = m.UserDirs
		if len(dirs) > 0 {
			dropInRoot = dirs[0]
		}
	}

	for _, dir := range dirs {
		unitPath := filepath.Join(dir, unitName)
		if _, err := os.Stat(unitPath); err == nil {
			return Resolution{
				ServiceName: name,
				Scope:       scope,
				Systemd: &SystemdResolution{
					UnitName:   unitName,
					UnitPath:   unitPath,
					DropInRoot: dropInRoot,
				},
			}, nil
		}
	}

	return Resolution{}, fmt.Errorf("unit %s not found in %s scope (searched: %s)",
		unitName, scope, strings.Join(dirs, ", "))
}

func (m *SystemdManager) Inject(res Resolution, cfg EnvConfig) (InjectResult, error) {
	sd := res.Systemd
	if sd == nil {
		return InjectResult{}, fmt.Errorf("not a systemd resolution")
	}

	dropInDir := filepath.Join(sd.DropInRoot, sd.UnitName+".d")
	dropInPath := filepath.Join(dropInDir, dropInFileName)

	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		return InjectResult{Path: dropInPath}, fmt.Errorf("create drop-in dir: %w", err)
	}

	content := generateSystemdDropIn(cfg)
	if err := os.WriteFile(dropInPath, []byte(content), 0644); err != nil {
		return InjectResult{Path: dropInPath}, fmt.Errorf("write drop-in: %w", err)
	}

	return InjectResult{Path: dropInPath, Written: true}, nil
}

func (m *SystemdManager) Remove(res Resolution, _ EnvConfig) (RemoveResult, error) {
	sd := res.Systemd
	if sd == nil {
		return RemoveResult{}, fmt.Errorf("not a systemd resolution")
	}

	dropInDir := filepath.Join(sd.DropInRoot, sd.UnitName+".d")
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

// CheckInjected returns true only if the drop-in file exists, begins with
// the expected header comment, and contains a non-empty HTTPS_PROXY assignment.
func (m *SystemdManager) CheckInjected(res Resolution) bool {
	sd := res.Systemd
	if sd == nil {
		return false
	}

	dropInPath := filepath.Join(sd.DropInRoot, sd.UnitName+".d", dropInFileName)
	data, err := os.ReadFile(dropInPath)
	if err != nil {
		return false
	}

	content := string(data)
	if !strings.HasPrefix(content, dropInHeader) {
		return false
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "HTTPS_PROXY=") && !strings.HasSuffix(strings.TrimSpace(line), `HTTPS_PROXY=""`) {
			return true
		}
	}
	return false
}

func (m *SystemdManager) Restart(res Resolution) error {
	if res.Scope == ScopeUser {
		if err := m.RunCmd("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl --user daemon-reload: %w", err)
		}
		return m.RunCmd("systemctl", "--user", "restart", res.ServiceName)
	}
	if err := m.RunCmd("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return m.RunCmd("systemctl", "restart", res.ServiceName)
}

func generateSystemdDropIn(cfg EnvConfig) string {
	var b strings.Builder
	b.WriteString(dropInHeader)
	b.WriteString("[Service]\n")
	for _, env := range ProxyEnvVars(cfg) {
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", env.Key, env.Value)
	}
	return b.String()
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/service -run 'TestSystemdManager_' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/systemd.go internal/service/systemd_test.go
git commit -m "feat: add SystemdManager implementing service.Manager"
```

---

### Task 4: Implement LaunchdManager

**Files:**
- Create: `internal/service/launchd.go`
- Test: `internal/service/launchd_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestLaunchdManager_Resolve_SystemScope`
- `TestLaunchdManager_Resolve_UserScope`
- `TestLaunchdManager_Resolve_NotFound`
- `TestLaunchdManager_Resolve_NoFallbackAcrossScopes`
- `TestLaunchdManager_Inject_GeneratesCorrectCommands`
- `TestLaunchdManager_Remove_GeneratesCorrectCommands`
- `TestLaunchdManager_CheckInjected_Present`
- `TestLaunchdManager_CheckInjected_Absent`
- `TestLaunchdManager_Restart_SystemScope`
- `TestLaunchdManager_Restart_UserScope`
- `TestLaunchdManager_Restart_NoDomainTarget`
- `TestLaunchdDomain_SystemScope`
- `TestLaunchdDomain_UserScope`

All tests override `RunCmd`, `GetOutput`, and `GetUID` on the manager struct. Resolve tests use temp directories with fake plist files and override `GetOutput` to stub PlistBuddy label reads.

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service -run 'TestLaunchdManager_|TestLaunchdDomain_' -v
```

Expected: FAIL because the implementation does not exist.

**Step 3: Write minimal implementation**

Create `internal/service/launchd.go`:

```go
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LaunchdManager implements Manager for launchd-based systems (macOS).
type LaunchdManager struct {
	SystemDirs []string
	UserDirs   []string
	RunCmd     func(name string, args ...string) error
	GetOutput  func(name string, args ...string) ([]byte, error)
	GetUID     func() int
}

// NewLaunchdManager returns a LaunchdManager with production defaults.
func NewLaunchdManager() *LaunchdManager {
	home, _ := os.UserHomeDir()
	return &LaunchdManager{
		SystemDirs: []string{"/Library/LaunchDaemons"},
		UserDirs:   []string{filepath.Join(home, "Library", "LaunchAgents")},
		RunCmd:     defaultRunCmd,
		GetOutput:  defaultGetOutput,
		GetUID:     os.Getuid,
	}
}

func (m *LaunchdManager) Resolve(name string, scope Scope) (Resolution, error) {
	dirs := m.SystemDirs
	if scope == ScopeUser {
		dirs = m.UserDirs
	}

	for _, dir := range dirs {
		plistPath := filepath.Join(dir, name+".plist")
		if _, err := os.Stat(plistPath); err == nil {
			label, err := m.readLabel(plistPath)
			if err != nil {
				return Resolution{}, err
			}
			domain := launchdDomain(scope, m.GetUID(), label)
			return Resolution{
				ServiceName: name,
				Scope:       scope,
				Launchd: &LaunchdResolution{
					PlistPath:    plistPath,
					Label:        label,
					DomainTarget: domain,
				},
			}, nil
		}
	}

	return Resolution{}, fmt.Errorf("plist %s.plist not found in %s scope (searched: %s)",
		name, scope, strings.Join(dirs, ", "))
}

func (m *LaunchdManager) Inject(res Resolution, cfg EnvConfig) (InjectResult, error) {
	ld := res.Launchd
	if ld == nil {
		return InjectResult{}, fmt.Errorf("not a launchd resolution")
	}

	cmds := generateLaunchdPlistCommands(ld.PlistPath, cfg)
	for _, c := range cmds {
		if err := m.RunCmd("sh", "-c", c); err != nil {
			return InjectResult{Path: ld.PlistPath}, fmt.Errorf("plist patch: %w", err)
		}
	}

	return InjectResult{Path: ld.PlistPath, Written: true}, nil
}

func (m *LaunchdManager) Remove(res Resolution, cfg EnvConfig) (RemoveResult, error) {
	ld := res.Launchd
	if ld == nil {
		return RemoveResult{}, fmt.Errorf("not a launchd resolution")
	}

	cmds := generateLaunchdRemoveCommands(ld.PlistPath, cfg)
	for _, c := range cmds {
		if err := m.RunCmd("sh", "-c", c); err != nil {
			return RemoveResult{Path: ld.PlistPath}, fmt.Errorf("plist remove: %w", err)
		}
	}

	return RemoveResult{Path: ld.PlistPath, Removed: true}, nil
}

// CheckInjected returns true only if the plist contains a non-empty
// HTTPS_PROXY in EnvironmentVariables.
func (m *LaunchdManager) CheckInjected(res Resolution) bool {
	ld := res.Launchd
	if ld == nil {
		return false
	}

	out, err := m.GetOutput(
		"/usr/libexec/PlistBuddy", "-c", "Print :EnvironmentVariables:HTTPS_PROXY", ld.PlistPath,
	)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func (m *LaunchdManager) Restart(res Resolution) error {
	ld := res.Launchd
	if ld == nil {
		return fmt.Errorf("not a launchd resolution")
	}
	if ld.DomainTarget == "" {
		return fmt.Errorf("no domain target resolved for launchd restart")
	}
	return m.RunCmd("launchctl", "kickstart", "-k", ld.DomainTarget)
}

func (m *LaunchdManager) readLabel(plistPath string) (string, error) {
	out, err := m.GetOutput("/usr/libexec/PlistBuddy", "-c", "Print :Label", plistPath)
	if err != nil {
		return "", fmt.Errorf("read Label from %s: %w", plistPath, err)
	}
	label := strings.TrimSpace(string(out))
	if label == "" {
		return "", fmt.Errorf("empty Label in %s", plistPath)
	}
	return label, nil
}

func generateLaunchdPlistCommands(plistPath string, cfg EnvConfig) []string {
	envVars := ProxyEnvVars(cfg)
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

func generateLaunchdRemoveCommands(plistPath string, cfg EnvConfig) []string {
	envVars := ProxyEnvVars(cfg)
	var cmds []string
	for _, env := range envVars {
		cmds = append(cmds, fmt.Sprintf(
			"/usr/libexec/PlistBuddy -c 'Delete :EnvironmentVariables:%s' %s 2>/dev/null || true",
			env.Key, plistPath,
		))
	}
	return cmds
}

// launchdDomain returns the launchctl domain target for the given scope.
func launchdDomain(scope Scope, uid int, label string) string {
	if scope == ScopeSystem {
		return "system/" + label
	}
	return fmt.Sprintf("gui/%d/%s", uid, label)
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/service -run 'TestLaunchdManager_|TestLaunchdDomain_' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/launchd.go internal/service/launchd_test.go
git commit -m "feat: add LaunchdManager implementing service.Manager"
```

---

### Task 5: Add manager detection and privilege guards

**Files:**
- Create: `internal/service/detect.go`
- Test: `internal/service/detect_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `TestDetectManagerForOS_Linux`
- `TestDetectManagerForOS_Darwin`
- `TestDetectManagerForOS_Unknown`
- `TestValidatePrivileges_SystemRequiresRoot`
- `TestValidatePrivileges_SystemAllowsRoot`
- `TestValidatePrivileges_UserAllowsNonRoot`
- `TestValidatePrivileges_UserRejectsSudo`
- `TestSuggestElevatedCommand`

Suggested test shape:

```go
func TestDetectManagerForOS_Linux(t *testing.T) {
	mgr := detectManagerForOS("linux")
	require.IsType(t, &SystemdManager{}, mgr)
}

func TestDetectManagerForOS_Darwin(t *testing.T) {
	mgr := detectManagerForOS("darwin")
	require.IsType(t, &LaunchdManager{}, mgr)
}

func TestDetectManagerForOS_Unknown(t *testing.T) {
	mgr := detectManagerForOS("windows")
	require.Nil(t, mgr)
}

func TestValidatePrivileges_UserRejectsSudo(t *testing.T) {
	err := ValidatePrivileges(ScopeUser, 0, "alice")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not through sudo")
	require.Contains(t, err.Error(), "alice")
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service -run 'TestDetectManagerForOS_|TestValidatePrivileges_|TestSuggestElevatedCommand' -v
```

Expected: FAIL because detect.go does not exist.

**Step 3: Write minimal implementation**

Create `internal/service/detect.go`:

```go
package service

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// DetectManager returns the Manager implementation for the current OS.
// Returns nil if the OS is not supported.
func DetectManager() Manager {
	return detectManagerForOS(runtime.GOOS)
}

func detectManagerForOS(goos string) Manager {
	switch goos {
	case "linux":
		return NewSystemdManager()
	case "darwin":
		return NewLaunchdManager()
	default:
		return nil
	}
}

// ValidatePrivileges checks whether the current process has the right
// privilege level for the requested scope.
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

// SuggestElevatedCommand returns the sudo-prefixed version of the given args.
func SuggestElevatedCommand(args []string) string {
	out := "sudo"
	for _, a := range args {
		out += " " + a
	}
	return out
}

func defaultRunCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func defaultGetOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/service -run 'TestDetectManagerForOS_|TestValidatePrivileges_|TestSuggestElevatedCommand' -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/detect.go internal/service/detect_test.go
git commit -m "feat: add manager detection and privilege guards"
```

---

### Task 6: Add `crabwise service` CLI with `--agent` and `--scope`

**Files:**
- Create: `internal/cli/service.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/daemon/config.go` (add `ServiceConfig` with agent registry)
- Test: `internal/cli/service_test.go`

**Step 1: Add agent registry to daemon config**

Add to `internal/daemon/config.go`:

```go
type ServiceConfig struct {
	Agents map[string]service.AgentServiceEntry `yaml:"agents"`
}
```

Add `Service ServiceConfig \`yaml:"service"\`` to the top-level `Config` struct. Add defaults:

```go
cfg.Service.Agents = map[string]service.AgentServiceEntry{
	"openclaw": {SystemdUnit: "openclaw-gateway", LaunchdPlist: "com.openclaw.gateway"},
}
```

**Step 2: Write the failing tests**

Add tests for:

- `TestServiceCommand_DefaultScopeIsSystem`
- `TestServiceInjectCmd_DryRun_SystemScope`
- `TestServiceInjectCmd_DryRun_UserScope`
- `TestServiceStatusCmd_NotFound`
- `TestServiceStatusCmd_NotInjected`
- `TestServiceRejectsSudoForUserScope`
- `TestEnvConfigFromDaemonConfig`
- `TestServiceAgentRegistryLookup`
- `TestServiceAgentLiteralFallback`

**Step 2: Run test to verify it fails**

```bash
go test ./internal/cli -run 'TestService(Command_|InjectCmd_|StatusCmd_|RejectsSudo)|TestEnvConfigFromDaemonConfig' -v
```

Expected: FAIL because the command does not exist yet.

**Step 3: Write minimal implementation**

Create `internal/cli/service.go`:

```go
package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/service"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage proxy injection for system and user services",
	}

	cmd.AddCommand(
		newServiceInjectCmd(),
		newServiceRemoveCmd(),
		newServiceStatusCmd(),
	)

	return cmd
}

func newServiceInjectCmd() *cobra.Command {
	var scopeFlag, agentName, configPath string
	var restart bool

	cmd := &cobra.Command{
		Use:   "inject",
		Short: "Inject proxy environment into a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scope, err := service.ParseScope(scopeFlag)
			if err != nil {
				return err
			}

			if err := service.ValidatePrivileges(scope, os.Getuid(), os.Getenv("SUDO_USER")); err != nil {
				if scope == service.ScopeSystem {
					fmt.Fprintf(os.Stderr, "hint: %s\n",
						service.SuggestElevatedCommand(os.Args))
				}
				return err
			}

			mgr := service.DetectManager()
			if mgr == nil {
				return fmt.Errorf("unsupported operating system")
			}

			serviceName := service.ResolveAgentName(agentName, cfg.Service.Agents, runtime.GOOS)
			res, err := mgr.Resolve(serviceName, scope)
			if err != nil {
				return err
			}

			envCfg := envConfigFromDaemon(cfg)
			result, err := mgr.Inject(res, envCfg)
			if err != nil {
				return err
			}

			if isPlain() {
				fmt.Printf("injected: %s\n", result.Path)
			} else {
				fmt.Printf("  %s %s %s\n",
					tui.StatusIcon("success"),
					tui.StyleBody.Render("Proxy injected"),
					tui.StyleMuted.Render(result.Path))
			}

			if restart {
				if err := mgr.Restart(res); err != nil {
					return fmt.Errorf("inject succeeded but restart failed: %w", err)
				}
				if isPlain() {
					fmt.Printf("restarted: %s\n", serviceName)
				} else {
					fmt.Printf("  %s %s\n",
						tui.StatusIcon("success"),
						tui.StyleBody.Render("Service restarted"))
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&scopeFlag, "scope", "system", "service scope: system or user")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent name or literal service name (required)")
	_ = cmd.MarkFlagRequired("agent")
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart service after inject")
	return cmd
}

func newServiceRemoveCmd() *cobra.Command {
	var scopeFlag, agentName, configPath string
	var restart bool

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove proxy injection from a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scope, err := service.ParseScope(scopeFlag)
			if err != nil {
				return err
			}

			if err := service.ValidatePrivileges(scope, os.Getuid(), os.Getenv("SUDO_USER")); err != nil {
				if scope == service.ScopeSystem {
					fmt.Fprintf(os.Stderr, "hint: %s\n",
						service.SuggestElevatedCommand(os.Args))
				}
				return err
			}

			mgr := service.DetectManager()
			if mgr == nil {
				return fmt.Errorf("unsupported operating system")
			}

			serviceName := service.ResolveAgentName(agentName, cfg.Service.Agents, runtime.GOOS)
			res, err := mgr.Resolve(serviceName, scope)
			if err != nil {
				return err
			}

			envCfg := envConfigFromDaemon(cfg)
			result, err := mgr.Remove(res, envCfg)
			if err != nil {
				return err
			}

			if isPlain() {
				if result.Removed {
					fmt.Printf("removed: %s\n", result.Path)
				} else {
					fmt.Printf("not injected: %s\n", result.Path)
				}
			} else {
				if result.Removed {
					fmt.Printf("  %s %s %s\n",
						tui.StatusIcon("success"),
						tui.StyleBody.Render("Proxy removed"),
						tui.StyleMuted.Render(result.Path))
				} else {
					fmt.Printf("  %s %s\n",
						tui.StatusIcon("warning"),
						tui.StyleBody.Render("Not injected — nothing to remove"))
				}
			}

			if restart && result.Removed {
				if err := mgr.Restart(res); err != nil {
					return fmt.Errorf("remove succeeded but restart failed: %w", err)
				}
				if isPlain() {
					fmt.Printf("restarted: %s\n", serviceName)
				} else {
					fmt.Printf("  %s %s\n",
						tui.StatusIcon("success"),
						tui.StyleBody.Render("Service restarted"))
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&scopeFlag, "scope", "system", "service scope: system or user")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent name or literal service name (required)")
	_ = cmd.MarkFlagRequired("agent")
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart service after remove")
	return cmd
}

func newServiceStatusCmd() *cobra.Command {
	var scopeFlag, agentName, configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show proxy injection status for a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scope, err := service.ParseScope(scopeFlag)
			if err != nil {
				return err
			}

			mgr := service.DetectManager()
			if mgr == nil {
				return fmt.Errorf("unsupported operating system")
			}

			serviceName := service.ResolveAgentName(agentName, cfg.Service.Agents, runtime.GOOS)
			res, err := mgr.Resolve(serviceName, scope)
			if err != nil {
				if isPlain() {
					fmt.Printf("agent: %s\nscope: %s\nservice: %s\nresolved: false\n",
						agentName, scope, serviceName)
				} else {
					fmt.Printf("  %s %s %s\n",
						tui.StatusIcon("warning"),
						tui.StyleBody.Render("Not found"),
						tui.StyleMuted.Render(serviceName+" in "+string(scope)+" scope"))
				}
				return nil
			}

			injected := mgr.CheckInjected(res)

			if isPlain() {
				fmt.Printf("agent: %s\nscope: %s\nservice: %s\nresolved: true\ninjected: %t\n",
					agentName, scope, serviceName, injected)
			} else {
				fmt.Printf("  %s %s %s\n",
					tui.StatusIcon("success"),
					tui.StyleBody.Render("Resolved"),
					tui.StyleMuted.Render(agentName+" → "+serviceName+" in "+string(scope)+" scope"))
				fmt.Printf("  %s %s\n",
					tui.StatusIcon(boolToStatus(injected)),
					tui.StyleBody.Render("Proxy "+map[bool]string{true: "injected", false: "not injected"}[injected]))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&scopeFlag, "scope", "system", "service scope: system or user")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent name or literal service name (required)")
	_ = cmd.MarkFlagRequired("agent")
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}
```

Key patterns:

- `--agent` is required; resolves via config registry, falls back to literal unit name
- `--scope` defaults to `system`
- Privilege check runs before resolution; if system scope fails, prints the exact `sudo` command
- `status` does not require privilege because it only reads
- Uses `isPlain()` and `tui.*` styles matching the existing CLI pattern (see `cert.go`)
- Inject/remove report restart failures separately from operation success

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

### Task 7: Update docs for system and user service domains

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
# Production daemon (resolves via registry, system scope default)
sudo crabwise service inject --agent openclaw --restart

# Per-user agent (literal fallback, user scope)
crabwise service inject --scope user --agent my-agent --restart

# Check injection status
crabwise service status --agent openclaw
crabwise service status --scope user --agent my-agent

# Remove injection
sudo crabwise service remove --agent openclaw --restart
crabwise service remove --scope user --agent my-agent --restart
```

In `docs/plans/proxy_enforcement-howto.md`, update the OpenClaw section to show both patterns:

- `crabwise wrap -- openclaw gateway` for interactive/dev mode
- `sudo crabwise service inject --agent openclaw --restart` for production daemon installs
- Note that user-scoped agents should use `--scope user` and not `sudo`

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

### Task 8: Final verification pass

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

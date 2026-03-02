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
type Manager interface {
	Resolve(name string, scope Scope) (Resolution, error)
	Inject(res Resolution, cfg EnvConfig) (InjectResult, error)
	Remove(res Resolution, cfg EnvConfig) (RemoveResult, error)
	CheckInjected(res Resolution) (bool, error)
	Restart(res Resolution) error
}

// AgentServiceEntry maps a friendly agent name to platform-specific service names.
type AgentServiceEntry struct {
	SystemdUnit  string `yaml:"systemd_unit"`
	LaunchdPlist string `yaml:"launchd_plist"`
}

// ResolveAgentName looks up a friendly agent name in the registry and returns
// the platform-specific service name. If not in the registry, returns as-is.
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

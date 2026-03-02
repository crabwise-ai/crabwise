package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LaunchdManager implements Manager for launchd-based macOS systems.
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

func validatePlistBuddyValue(value string) error {
	if strings.Contains(value, ";") {
		return fmt.Errorf("value %q contains semicolon, which PlistBuddy uses as a command separator", value)
	}
	return nil
}

func (m *LaunchdManager) Inject(res Resolution, cfg EnvConfig) (InjectResult, error) {
	ld := res.Launchd
	if ld == nil {
		return InjectResult{}, fmt.Errorf("not a launchd resolution")
	}

	envVars := ProxyEnvVars(cfg)
	for _, env := range envVars {
		if err := validatePlistBuddyValue(env.Value); err != nil {
			return InjectResult{Path: ld.PlistPath}, fmt.Errorf("unsafe value for %s: %w", env.Key, err)
		}
	}

	_ = m.RunCmd("/usr/libexec/PlistBuddy", "-c", "Add :EnvironmentVariables dict", ld.PlistPath)

	for _, env := range envVars {
		setCmd := fmt.Sprintf("Set :EnvironmentVariables:%s %s", env.Key, env.Value)
		if err := m.RunCmd("/usr/libexec/PlistBuddy", "-c", setCmd, ld.PlistPath); err != nil {
			addCmd := fmt.Sprintf("Add :EnvironmentVariables:%s string %s", env.Key, env.Value)
			if err := m.RunCmd("/usr/libexec/PlistBuddy", "-c", addCmd, ld.PlistPath); err != nil {
				return InjectResult{Path: ld.PlistPath}, fmt.Errorf("plist patch %s: %w", env.Key, err)
			}
		}
	}

	return InjectResult{Path: ld.PlistPath, Written: true}, nil
}

func (m *LaunchdManager) Remove(res Resolution, cfg EnvConfig) (RemoveResult, error) {
	ld := res.Launchd
	if ld == nil {
		return RemoveResult{}, fmt.Errorf("not a launchd resolution")
	}

	injected, err := m.CheckInjected(res)
	if err != nil {
		return RemoveResult{Path: ld.PlistPath}, fmt.Errorf("check before remove: %w", err)
	}
	if !injected {
		return RemoveResult{Path: ld.PlistPath, Removed: false}, nil
	}

	for _, env := range ProxyEnvVars(cfg) {
		_ = m.RunCmd("/usr/libexec/PlistBuddy", "-c",
			fmt.Sprintf("Delete :EnvironmentVariables:%s", env.Key),
			ld.PlistPath)
	}

	return RemoveResult{Path: ld.PlistPath, Removed: true}, nil
}

func (m *LaunchdManager) CheckInjected(res Resolution) (bool, error) {
	ld := res.Launchd
	if ld == nil {
		return false, fmt.Errorf("not a launchd resolution")
	}

	out, err := m.GetOutput(
		"/usr/libexec/PlistBuddy", "-c", "Print :EnvironmentVariables:HTTPS_PROXY", ld.PlistPath,
	)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) &&
			strings.Contains(string(exitErr.Stderr), "Does Not Exist") {
			return false, nil
		}
		return false, fmt.Errorf("check plist %s: %w", ld.PlistPath, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (m *LaunchdManager) Restart(res Resolution) error {
	ld := res.Launchd
	if ld == nil {
		return fmt.Errorf("not a launchd resolution")
	}
	if ld.DomainTarget == "" {
		return fmt.Errorf("no domain target resolved for launchd restart")
	}

	_ = m.RunCmd("launchctl", "bootout", ld.DomainTarget)

	domainPrefix := launchdDomainPrefix(res.Scope, m.GetUID())
	return m.RunCmd("launchctl", "bootstrap", domainPrefix, ld.PlistPath)
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

func launchdDomain(scope Scope, uid int, label string) string {
	if scope == ScopeSystem {
		return "system/" + label
	}
	return fmt.Sprintf("gui/%d/%s", uid, label)
}

func launchdDomainPrefix(scope Scope, uid int) string {
	if scope == ScopeSystem {
		return "system"
	}
	return fmt.Sprintf("gui/%d", uid)
}

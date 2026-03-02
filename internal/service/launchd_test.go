package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type cmdCall struct {
	Name string
	Args []string
}

func fakeLaunchdManager(systemDirs, userDirs []string) (*LaunchdManager, *[]cmdCall) {
	var calls []cmdCall
	return &LaunchdManager{
		SystemDirs: systemDirs,
		UserDirs:   userDirs,
		RunCmd: func(name string, args ...string) error {
			calls = append(calls, cmdCall{Name: name, Args: args})
			return nil
		},
		GetOutput: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, cmdCall{Name: name, Args: args})
			return nil, nil
		},
		GetUID: func() int { return 501 },
	}, &calls
}

func writeFakePlist(t *testing.T, dir, name, label string) string {
	t.Helper()
	plistPath := filepath.Join(dir, name+".plist")
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
</dict>
</plist>`, label)
	require.NoError(t, os.WriteFile(plistPath, []byte(content), 0644))
	return plistPath
}

func makeExitErrorWithStderr(stderr string) *exec.ExitError {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("echo -n %q >&2; exit 1", stderr))
	_, err := cmd.Output()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		panic(fmt.Sprintf("expected *exec.ExitError, got %T", err))
	}
	return exitErr
}

// --- Resolve ---

func TestLaunchdManager_Resolve_SystemScope(t *testing.T) {
	dir := t.TempDir()
	label := "com.example.daemon"
	writeFakePlist(t, dir, "com.example.daemon", label)

	m, _ := fakeLaunchdManager([]string{dir}, nil)
	m.GetOutput = func(name string, args ...string) ([]byte, error) {
		return []byte(label + "\n"), nil
	}

	res, err := m.Resolve("com.example.daemon", ScopeSystem)
	require.NoError(t, err)
	require.Equal(t, "com.example.daemon", res.ServiceName)
	require.Equal(t, ScopeSystem, res.Scope)
	require.NotNil(t, res.Launchd)
	require.Equal(t, filepath.Join(dir, "com.example.daemon.plist"), res.Launchd.PlistPath)
	require.Equal(t, label, res.Launchd.Label)
	require.Equal(t, "system/com.example.daemon", res.Launchd.DomainTarget)
}

func TestLaunchdManager_Resolve_UserScope(t *testing.T) {
	dir := t.TempDir()
	label := "com.example.agent"
	writeFakePlist(t, dir, "com.example.agent", label)

	m, _ := fakeLaunchdManager(nil, []string{dir})
	m.GetOutput = func(name string, args ...string) ([]byte, error) {
		return []byte(label + "\n"), nil
	}

	res, err := m.Resolve("com.example.agent", ScopeUser)
	require.NoError(t, err)
	require.Equal(t, ScopeUser, res.Scope)
	require.NotNil(t, res.Launchd)
	require.Equal(t, label, res.Launchd.Label)
	require.Equal(t, "gui/501/com.example.agent", res.Launchd.DomainTarget)
}

func TestLaunchdManager_Resolve_NotFound(t *testing.T) {
	dir := t.TempDir()
	m, _ := fakeLaunchdManager([]string{dir}, []string{dir})

	_, err := m.Resolve("nonexistent", ScopeSystem)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestLaunchdManager_Resolve_NoFallbackAcrossScopes(t *testing.T) {
	systemDir := t.TempDir()
	userDir := t.TempDir()
	writeFakePlist(t, systemDir, "com.example.daemon", "com.example.daemon")

	m, _ := fakeLaunchdManager([]string{systemDir}, []string{userDir})
	m.GetOutput = func(name string, args ...string) ([]byte, error) {
		return []byte("com.example.daemon\n"), nil
	}

	_, err := m.Resolve("com.example.daemon", ScopeUser)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// --- Inject ---

func TestLaunchdManager_Inject_CallsPlistBuddyDirectly(t *testing.T) {
	m, calls := fakeLaunchdManager(nil, nil)
	res := Resolution{
		Launchd: &LaunchdResolution{PlistPath: "/tmp/test.plist", Label: "com.test"},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119"}

	result, err := m.Inject(res, cfg)
	require.NoError(t, err)
	require.True(t, result.Written)

	for _, c := range *calls {
		require.Equal(t, "/usr/libexec/PlistBuddy", c.Name)
		require.NotEqual(t, "sh", c.Name)
		require.NotEqual(t, "bash", c.Name)
	}
}

func TestLaunchdManager_Inject_NoShellInterpolation(t *testing.T) {
	m, calls := fakeLaunchdManager(nil, nil)
	res := Resolution{
		Launchd: &LaunchdResolution{PlistPath: "/tmp/test.plist", Label: "com.test"},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119"}

	_, err := m.Inject(res, cfg)
	require.NoError(t, err)

	shellMeta := []string{"$", "`", "(", ")", "{", "}"}
	for _, c := range *calls {
		for _, arg := range c.Args {
			for _, meta := range shellMeta {
				require.NotContains(t, arg, meta,
					"arg %q contains shell metacharacter %q", arg, meta)
			}
		}
	}
}

func TestLaunchdManager_Inject_RejectsSemicolonInValue(t *testing.T) {
	m, _ := fakeLaunchdManager(nil, nil)
	res := Resolution{
		Launchd: &LaunchdResolution{PlistPath: "/tmp/test.plist", Label: "com.test"},
	}
	cfg := EnvConfig{ProxyURL: "http://evil;rm -rf /"}

	_, err := m.Inject(res, cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "semicolon")
}

// --- Remove ---

func TestLaunchdManager_Remove_CallsPlistBuddyDirectly(t *testing.T) {
	m, calls := fakeLaunchdManager(nil, nil)
	m.GetOutput = func(name string, args ...string) ([]byte, error) {
		*calls = append(*calls, cmdCall{Name: name, Args: args})
		return []byte("http://127.0.0.1:9119\n"), nil
	}
	res := Resolution{
		Launchd: &LaunchdResolution{PlistPath: "/tmp/test.plist", Label: "com.test"},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119"}

	result, err := m.Remove(res, cfg)
	require.NoError(t, err)
	require.True(t, result.Removed)

	hasPlistBuddyDelete := false
	for _, c := range *calls {
		require.NotEqual(t, "sh", c.Name)
		require.NotEqual(t, "bash", c.Name)
		if c.Name == "/usr/libexec/PlistBuddy" && len(c.Args) > 1 && strings.HasPrefix(c.Args[1], "Delete") {
			hasPlistBuddyDelete = true
		}
	}
	require.True(t, hasPlistBuddyDelete)
}

func TestLaunchdManager_Remove_NotInjected(t *testing.T) {
	m, _ := fakeLaunchdManager(nil, nil)
	exitErr := makeExitErrorWithStderr("Does Not Exist")
	m.GetOutput = func(name string, args ...string) ([]byte, error) {
		return nil, exitErr
	}
	res := Resolution{
		Launchd: &LaunchdResolution{PlistPath: "/tmp/test.plist", Label: "com.test"},
	}
	cfg := EnvConfig{ProxyURL: "http://127.0.0.1:9119"}

	result, err := m.Remove(res, cfg)
	require.NoError(t, err)
	require.False(t, result.Removed)
}

// --- CheckInjected ---

func TestLaunchdManager_CheckInjected_Present(t *testing.T) {
	m, _ := fakeLaunchdManager(nil, nil)
	m.GetOutput = func(name string, args ...string) ([]byte, error) {
		return []byte("http://127.0.0.1:9119\n"), nil
	}
	res := Resolution{
		Launchd: &LaunchdResolution{PlistPath: "/tmp/test.plist", Label: "com.test"},
	}

	injected, err := m.CheckInjected(res)
	require.NoError(t, err)
	require.True(t, injected)
}

func TestLaunchdManager_CheckInjected_Absent(t *testing.T) {
	exitErr := makeExitErrorWithStderr("Does Not Exist")
	m, _ := fakeLaunchdManager(nil, nil)
	m.GetOutput = func(name string, args ...string) ([]byte, error) {
		return nil, exitErr
	}
	res := Resolution{
		Launchd: &LaunchdResolution{PlistPath: "/tmp/test.plist", Label: "com.test"},
	}

	injected, err := m.CheckInjected(res)
	require.NoError(t, err)
	require.False(t, injected)
}

func TestLaunchdManager_CheckInjected_Error(t *testing.T) {
	m, _ := fakeLaunchdManager(nil, nil)
	m.GetOutput = func(name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("unexpected I/O error")
	}
	res := Resolution{
		Launchd: &LaunchdResolution{PlistPath: "/tmp/test.plist", Label: "com.test"},
	}

	_, err := m.CheckInjected(res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "check plist")
}

// --- Restart ---

func TestLaunchdManager_Restart_SystemScope_UsesBootoutBootstrap(t *testing.T) {
	m, calls := fakeLaunchdManager(nil, nil)
	res := Resolution{
		Scope: ScopeSystem,
		Launchd: &LaunchdResolution{
			PlistPath:    "/Library/LaunchDaemons/com.test.plist",
			Label:        "com.test",
			DomainTarget: "system/com.test",
		},
	}

	err := m.Restart(res)
	require.NoError(t, err)
	require.Len(t, *calls, 2)

	require.Equal(t, "launchctl", (*calls)[0].Name)
	require.Equal(t, []string{"bootout", "system/com.test"}, (*calls)[0].Args)

	require.Equal(t, "launchctl", (*calls)[1].Name)
	require.Equal(t, []string{"bootstrap", "system", "/Library/LaunchDaemons/com.test.plist"}, (*calls)[1].Args)
}

func TestLaunchdManager_Restart_UserScope_UsesBootoutBootstrap(t *testing.T) {
	m, calls := fakeLaunchdManager(nil, nil)
	res := Resolution{
		Scope: ScopeUser,
		Launchd: &LaunchdResolution{
			PlistPath:    "/Users/test/Library/LaunchAgents/com.test.plist",
			Label:        "com.test",
			DomainTarget: "gui/501/com.test",
		},
	}

	err := m.Restart(res)
	require.NoError(t, err)
	require.Len(t, *calls, 2)

	require.Equal(t, "launchctl", (*calls)[0].Name)
	require.Equal(t, []string{"bootout", "gui/501/com.test"}, (*calls)[0].Args)

	require.Equal(t, "launchctl", (*calls)[1].Name)
	require.Equal(t, []string{"bootstrap", "gui/501", "/Users/test/Library/LaunchAgents/com.test.plist"}, (*calls)[1].Args)
}

func TestLaunchdManager_Restart_NoDomainTarget(t *testing.T) {
	m, _ := fakeLaunchdManager(nil, nil)
	res := Resolution{
		Launchd: &LaunchdResolution{
			PlistPath:    "/tmp/test.plist",
			Label:        "com.test",
			DomainTarget: "",
		},
	}

	err := m.Restart(res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no domain target")
}

// --- Domain helpers ---

func TestLaunchdDomain_SystemScope(t *testing.T) {
	require.Equal(t, "system/com.test", launchdDomain(ScopeSystem, 501, "com.test"))
}

func TestLaunchdDomain_UserScope(t *testing.T) {
	require.Equal(t, "gui/501/com.test", launchdDomain(ScopeUser, 501, "com.test"))
}

func TestLaunchdDomainPrefix_SystemScope(t *testing.T) {
	require.Equal(t, "system", launchdDomainPrefix(ScopeSystem, 501))
}

func TestLaunchdDomainPrefix_UserScope(t *testing.T) {
	require.Equal(t, "gui/501", launchdDomainPrefix(ScopeUser, 501))
}

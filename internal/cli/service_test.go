package cli

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/service"
	"github.com/stretchr/testify/require"
)

// mockManager implements service.Manager for CLI integration tests.
type mockManager struct {
	resolveRes service.Resolution
	resolveErr error
	injectRes  service.InjectResult
	injectErr  error
	checkVal   bool
	checkErr   error
}

func (m *mockManager) Resolve(string, service.Scope) (service.Resolution, error) {
	return m.resolveRes, m.resolveErr
}
func (m *mockManager) Inject(service.Resolution, service.EnvConfig) (service.InjectResult, error) {
	return m.injectRes, m.injectErr
}
func (m *mockManager) Remove(service.Resolution, service.EnvConfig) (service.RemoveResult, error) {
	return service.RemoveResult{}, nil
}
func (m *mockManager) CheckInjected(service.Resolution) (bool, error) {
	return m.checkVal, m.checkErr
}
func (m *mockManager) Restart(service.Resolution) error { return nil }

func withMockManager(t *testing.T, mgr service.Manager) {
	t.Helper()
	orig := detectManagerFn
	detectManagerFn = func() service.Manager { return mgr }
	t.Cleanup(func() { detectManagerFn = orig })
}

func withUID(t *testing.T, uid int) {
	t.Helper()
	orig := getUIDFn
	getUIDFn = func() int { return uid }
	t.Cleanup(func() { getUIDFn = orig })
}

func withSUDOUser(t *testing.T, user string) {
	t.Helper()
	orig := getSUDOUserFn
	getSUDOUserFn = func() string { return user }
	t.Cleanup(func() { getSUDOUserFn = orig })
}

// nonexistentConfig returns a path to a non-existent config file.
// LoadConfig falls back to hardcoded defaults when the file is absent.
func nonexistentConfig(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.yaml")
}

func TestServiceCommand_DefaultScopeIsSystem(t *testing.T) {
	cmd := newServiceInjectCmd()
	flag := cmd.Flags().Lookup("scope")
	require.NotNil(t, flag)
	require.Equal(t, "system", flag.DefValue)
}

func TestServiceCommand_AgentRequired(t *testing.T) {
	cmd := newServiceInjectCmd()
	flag := cmd.Flags().Lookup("agent")
	require.NotNil(t, flag)

	annotations := flag.Annotations
	_, required := annotations["cobra_annotation_bash_completion_one_required_flag"]
	require.True(t, required, "--agent should be marked required")
}

func TestServiceCommand_HasSubcommands(t *testing.T) {
	cmd := newServiceCmd()
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	require.ElementsMatch(t, []string{"inject", "remove", "status"}, names)
}

func TestEnvConfigFromDaemonConfig(t *testing.T) {
	cfg := &daemon.Config{}
	cfg.Adapters.Proxy.Listen = "127.0.0.1:9119"
	cfg.Adapters.Proxy.CACert = "/tmp/ca.crt"

	envCfg := envConfigFromDaemon(cfg)
	require.Equal(t, "http://127.0.0.1:9119", envCfg.ProxyURL)
	require.Equal(t, "/tmp/ca.crt", envCfg.CACert)
}

func TestServiceAgentRegistryLookup(t *testing.T) {
	agents := map[string]service.AgentServiceEntry{
		"openclaw": {SystemdUnit: "openclaw-gateway", LaunchdPlist: "com.openclaw.gateway"},
	}
	require.Equal(t, "openclaw-gateway", service.ResolveAgentName("openclaw", agents, "linux"))
	require.Equal(t, "com.openclaw.gateway", service.ResolveAgentName("openclaw", agents, "darwin"))
}

func TestServiceAgentLiteralFallback(t *testing.T) {
	agents := map[string]service.AgentServiceEntry{
		"openclaw": {SystemdUnit: "openclaw-gateway"},
	}
	require.Equal(t, "my-custom-daemon", service.ResolveAgentName("my-custom-daemon", agents, "linux"))
}

func TestServiceInjectCmd_SystemScope(t *testing.T) {
	withMockManager(t, &mockManager{
		resolveRes: service.Resolution{ServiceName: "test-svc", Scope: service.ScopeSystem},
		injectRes:  service.InjectResult{Path: "/etc/systemd/system/test-svc.service.d/crabwise-proxy.conf", Written: true},
	})
	withUID(t, 0)
	withSUDOUser(t, "")

	cmd := newServiceInjectCmd()
	cmd.SetArgs([]string{"--agent", "test-svc", "--config", nonexistentConfig(t)})
	require.NoError(t, cmd.Execute())
}

func TestServiceInjectCmd_UserScope(t *testing.T) {
	withMockManager(t, &mockManager{
		resolveRes: service.Resolution{ServiceName: "test-svc", Scope: service.ScopeUser},
		injectRes:  service.InjectResult{Path: "/home/user/.config/systemd/user/test-svc.service.d/crabwise-proxy.conf", Written: true},
	})
	withUID(t, 1000)
	withSUDOUser(t, "")

	cmd := newServiceInjectCmd()
	cmd.SetArgs([]string{"--agent", "test-svc", "--scope", "user", "--config", nonexistentConfig(t)})
	require.NoError(t, cmd.Execute())
}

func TestServiceStatusCmd_NotFound(t *testing.T) {
	withMockManager(t, &mockManager{
		resolveErr: fmt.Errorf("unit test-svc.service not found in system scope"),
	})

	cmd := newServiceStatusCmd()
	cmd.SetArgs([]string{"--agent", "test-svc", "--config", nonexistentConfig(t)})
	// status returns nil even when service not found — prints "not found" message
	require.NoError(t, cmd.Execute())
}

func TestServiceStatusCmd_NotInjected(t *testing.T) {
	withMockManager(t, &mockManager{
		resolveRes: service.Resolution{ServiceName: "test-svc", Scope: service.ScopeSystem},
		checkVal:   false,
	})

	cmd := newServiceStatusCmd()
	cmd.SetArgs([]string{"--agent", "test-svc", "--config", nonexistentConfig(t)})
	require.NoError(t, cmd.Execute())
}

func TestServiceStatusCmd_CheckError(t *testing.T) {
	withMockManager(t, &mockManager{
		resolveRes: service.Resolution{ServiceName: "test-svc", Scope: service.ScopeSystem},
		checkErr:   fmt.Errorf("permission denied"),
	})

	cmd := newServiceStatusCmd()
	cmd.SetArgs([]string{"--agent", "test-svc", "--config", nonexistentConfig(t)})
	require.NoError(t, cmd.Execute())
}

func TestServiceRejectsRootForUserScope(t *testing.T) {
	withUID(t, 0)
	withSUDOUser(t, "alice")

	cmd := newServiceInjectCmd()
	cmd.SetArgs([]string{"--agent", "test-svc", "--scope", "user", "--config", nonexistentConfig(t)})
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "user scope requires a non-root user")
}

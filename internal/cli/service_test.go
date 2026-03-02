package cli

import (
	"testing"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/service"
	"github.com/stretchr/testify/require"
)

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

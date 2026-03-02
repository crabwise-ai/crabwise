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

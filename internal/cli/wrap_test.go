package cli

import (
	"testing"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/service"
)

func TestProxyEnvPairsIncludesUpperAndLowercaseProxyVars(t *testing.T) {
	cfg := &daemon.Config{}
	cfg.Adapters.Proxy.Listen = "127.0.0.1:9119"

	pairs := service.ProxyEnvVars(envConfigFromDaemon(cfg))
	values := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		values[pair.Key] = pair.Value
	}

	expectedProxyURL := "http://127.0.0.1:9119"
	for _, key := range []string{
		"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY",
		"https_proxy", "http_proxy", "all_proxy",
	} {
		if values[key] != expectedProxyURL {
			t.Fatalf("expected %s=%q, got %q", key, expectedProxyURL, values[key])
		}
	}

	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		if values[key] != "localhost,127.0.0.1" {
			t.Fatalf("expected %s localhost entries, got %q", key, values[key])
		}
	}
}

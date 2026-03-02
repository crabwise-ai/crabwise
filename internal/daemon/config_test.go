package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	if cfg.Queue.Capacity != 10000 {
		t.Fatalf("expected queue capacity 10000, got %d", cfg.Queue.Capacity)
	}
	if cfg.Queue.Overflow != "block_with_timeout" {
		t.Fatalf("expected block_with_timeout, got %s", cfg.Queue.Overflow)
	}
	if cfg.Audit.RawPayloadEnabled {
		t.Fatal("raw_payload_enabled should be false by default")
	}
	if cfg.Daemon.LogLevel != "info" {
		t.Fatalf("expected log_level info, got %s", cfg.Daemon.LogLevel)
	}
	if cfg.Commandments.File == "" {
		t.Fatal("expected commandments.file default to be set")
	}
	if cfg.ToolRegistry.File == "" {
		t.Fatal("expected tool_registry.file default to be set")
	}
	if len(cfg.Discovery.ProcessSignatures) == 0 {
		t.Fatal("expected discovery.process_signatures defaults")
	}
	if cfg.Adapters.Proxy.Listen == "" {
		t.Fatal("expected proxy listen default")
	}
	if cfg.Adapters.Proxy.MaxRequestBody <= 0 {
		t.Fatal("expected proxy max_request_body default")
	}
	hasCodex := false
	for _, sig := range cfg.Discovery.ProcessSignatures {
		if sig == "codex" {
			hasCodex = true
			break
		}
	}
	if !hasCodex {
		t.Fatalf("expected codex process signature in defaults, got %v", cfg.Discovery.ProcessSignatures)
	}
	hasCodexLogPath := false
	for _, p := range cfg.Discovery.LogPaths {
		if strings.Contains(p, ".codex") {
			hasCodexLogPath = true
			break
		}
	}
	if !hasCodexLogPath {
		t.Fatalf("expected codex log path in defaults, got %v", cfg.Discovery.LogPaths)
	}
}

func TestLoadConfig_ProxyEnabledRequiresDefaultProvider(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `
adapters:
  proxy:
    enabled: true
    default_provider: ""
`
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing default provider")
	}
}

func TestLoadConfig_ProxyConfiguredAuthRequiresKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `
adapters:
  proxy:
    enabled: true
    default_provider: openai
    providers:
      openai:
        upstream_base_url: https://api.openai.com
        auth_mode: configured
        route_patterns: ["/v1/responses"]
`
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for configured auth without auth_key")
	}
}

func TestLoadConfig_Override(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
queue:
  capacity: 5000
daemon:
  log_level: debug
`), 0600)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load override: %v", err)
	}

	if cfg.Queue.Capacity != 5000 {
		t.Fatalf("expected 5000, got %d", cfg.Queue.Capacity)
	}
	if cfg.Daemon.LogLevel != "debug" {
		t.Fatalf("expected debug, got %s", cfg.Daemon.LogLevel)
	}
}

func TestLoadConfig_InvalidLogLevel(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
daemon:
  log_level: verbose
`), 0600)

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid log_level")
	}
}

func TestLoadConfig_ExpandTilde(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(cfg.Daemon.SocketPath, home) {
		t.Fatalf("expected tilde expansion, got %s", cfg.Daemon.SocketPath)
	}
	if !strings.HasPrefix(cfg.Commandments.File, home) {
		t.Fatalf("expected commandments.file tilde expansion, got %s", cfg.Commandments.File)
	}
	if !strings.HasPrefix(cfg.ToolRegistry.File, home) {
		t.Fatalf("expected tool_registry.file tilde expansion, got %s", cfg.ToolRegistry.File)
	}
}

func TestLoadConfig_CommandmentsDefaultPathWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("commandments:\n  file: \"\"\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if filepath.Base(cfg.Commandments.File) != "commandments.yaml" {
		t.Fatalf("expected default commandments path, got %s", cfg.Commandments.File)
	}
}

func TestLoadConfig_ToolRegistryDefaultPathWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("tool_registry:\n  file: \"\"\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if filepath.Base(cfg.ToolRegistry.File) != "tool_registry.yaml" {
		t.Fatalf("expected default tool registry path, got %s", cfg.ToolRegistry.File)
	}
}

func TestLoadConfig_OpenClawDefaults(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path.yaml")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	if cfg.Adapters.OpenClaw.Enabled {
		t.Fatal("expected adapters.openclaw.enabled to default false")
	}
	if cfg.Adapters.OpenClaw.GatewayURL != "ws://127.0.0.1:18789" {
		t.Fatalf("expected default gateway_url, got %q", cfg.Adapters.OpenClaw.GatewayURL)
	}
	if cfg.Adapters.OpenClaw.APITokenEnv != "OPENCLAW_API_TOKEN" {
		t.Fatalf("expected default api_token_env, got %q", cfg.Adapters.OpenClaw.APITokenEnv)
	}
	if cfg.Adapters.OpenClaw.SessionRefreshInterval.Duration() != 30*time.Second {
		t.Fatalf("expected default session_refresh_interval 30s, got %s", cfg.Adapters.OpenClaw.SessionRefreshInterval.Duration())
	}
	if cfg.Adapters.OpenClaw.CorrelationWindow.Duration() != 3*time.Second {
		t.Fatalf("expected default correlation_window 3s, got %s", cfg.Adapters.OpenClaw.CorrelationWindow.Duration())
	}
}

func TestLoadConfig_OpenClawValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name: "gateway url required",
			content: `
adapters:
  openclaw:
    enabled: true
    gateway_url: ""
`,
			wantErr: "adapters.openclaw.gateway_url required when openclaw enabled",
		},
		{
			name: "session refresh interval must be positive",
			content: `
adapters:
  openclaw:
    enabled: true
    session_refresh_interval: 0s
`,
			wantErr: "adapters.openclaw.session_refresh_interval must be > 0 when openclaw enabled",
		},
		{
			name: "correlation window must be positive",
			content: `
adapters:
  openclaw:
    enabled: true
    correlation_window: 0s
`,
			wantErr: "adapters.openclaw.correlation_window must be > 0 when openclaw enabled",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tc.content), 0600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			_, err := LoadConfig(cfgPath)
			if err == nil {
				t.Fatalf("expected error %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

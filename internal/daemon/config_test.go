package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

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

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReloadRuntime_ReturnsCombinedErrorWhenBothReloadsFail(t *testing.T) {
	dir := t.TempDir()

	invalidCommandmentsPath := filepath.Join(dir, "commandments-invalid.yaml")
	if err := os.WriteFile(invalidCommandmentsPath, []byte("rules:\n  - :"), 0600); err != nil {
		t.Fatalf("write invalid commandments: %v", err)
	}

	invalidRegistryPath := filepath.Join(dir, "registry-invalid.yaml")
	if err := os.WriteFile(invalidRegistryPath, []byte("providers:\n  openai:\n    tools:\n      bad: ["), 0600); err != nil {
		t.Fatalf("write invalid registry: %v", err)
	}

	d := &Daemon{
		cfg: &Config{
			Commandments: CommandmentsConfig{File: invalidCommandmentsPath},
			ToolRegistry: ToolRegistryConfig{File: invalidRegistryPath},
		},
	}

	_, err := d.reloadRuntime()
	if err == nil {
		t.Fatal("expected combined runtime reload error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "reload commandments") {
		t.Fatalf("expected commandments error context, got: %s", msg)
	}
	if !strings.Contains(msg, "reload tool registry") {
		t.Fatalf("expected tool registry error context, got: %s", msg)
	}
}

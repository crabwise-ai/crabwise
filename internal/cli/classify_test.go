package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyCommand(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "tool_registry.yaml")
	registryYAML := `version: "1"
providers:
  openai:
    tools:
      bash:
        category: shell
        effect: execute
`
	if err := os.WriteFile(registryPath, []byte(registryYAML), 0600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfgYAML := "tool_registry:\n  file: " + registryPath + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := captureStdout(func() error {
		cmd := newClassifyCmd()
		cmd.SetArgs([]string{"Bash", "--config", cfgPath, "--provider", "openai", "--args", "command,file_path"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute classify command: %v", err)
	}

	if !strings.Contains(out, "Category:  shell") || !strings.Contains(out, "Source:    exact") {
		t.Fatalf("expected shell exact output, got: %s", out)
	}
}

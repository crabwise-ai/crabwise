package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/daemon"
)

func TestRootRegistersCommandmentsCommand(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"commandments"})
	if err != nil {
		t.Fatalf("find commandments command: %v", err)
	}
	if cmd == nil || cmd.Name() != "commandments" {
		t.Fatalf("expected commandments command to be registered")
	}

	for _, sub := range []string{"list", "test", "reload"} {
		subCmd, _, subErr := root.Find([]string{"commandments", sub})
		if subErr != nil {
			t.Fatalf("find commandments %s command: %v", sub, subErr)
		}
		if subCmd == nil || subCmd.Name() != sub {
			t.Fatalf("expected subcommand %q to be registered", sub)
		}
	}
}

func TestRootRegistersClassifyCommand(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"classify"})
	if err != nil {
		t.Fatalf("find classify command: %v", err)
	}
	if cmd == nil || cmd.Name() != "classify" {
		t.Fatalf("expected classify command to be registered")
	}
}

func TestAuditCommandSupportsTriggeredAndOutcomeFlags(t *testing.T) {
	cmd := newAuditCmd()
	if cmd.Flag("triggered") == nil {
		t.Fatal("expected --triggered flag")
	}
	if cmd.Flag("outcome") == nil {
		t.Fatal("expected --outcome flag")
	}
}

func TestWriteDefaultFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.yaml")

	written, err := writeDefaultFile(path, []byte("first"), false)
	if err != nil {
		t.Fatalf("write first file: %v", err)
	}
	if !written {
		t.Fatal("expected initial write")
	}

	written, err = writeDefaultFile(path, []byte("second"), false)
	if err != nil {
		t.Fatalf("write existing without force: %v", err)
	}
	if written {
		t.Fatal("expected existing file to be skipped without --force")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "first" {
		t.Fatalf("expected unchanged content, got %q", string(data))
	}

	written, err = writeDefaultFile(path, []byte("third"), true)
	if err != nil {
		t.Fatalf("write with force: %v", err)
	}
	if !written {
		t.Fatal("expected overwrite with force")
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file after force: %v", err)
	}
	if string(data) != "third" {
		t.Fatalf("expected overwritten content, got %q", string(data))
	}
}

func TestWatchCommand_TextFallbackFlag(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfgPath, []byte("daemon:\n  socket_path: \"/tmp/cw.sock\"\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origText := runWatchTextMode
	origTUI := runWatchTUIMode
	t.Cleanup(func() {
		runWatchTextMode = origText
		runWatchTUIMode = origTUI
	})

	calledText := false
	calledTUI := false
	runWatchTextMode = func(_ *daemon.Config) error {
		calledText = true
		return nil
	}
	runWatchTUIMode = func(_ *daemon.Config) error {
		calledTUI = true
		return nil
	}

	cmd := newWatchCmd()
	cmd.SetArgs([]string{"--text", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute watch --text: %v", err)
	}

	if cmd.Flag("text") == nil {
		t.Fatal("expected --text flag to be registered")
	}
	if !calledText {
		t.Fatal("expected --text mode handler to run")
	}
	if calledTUI {
		t.Fatal("expected TUI mode handler to be skipped")
	}
}

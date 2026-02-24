package cli

import (
	"os"
	"path/filepath"
	"testing"
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

package proxy

import (
	"encoding/json"
	"testing"
)

func TestParseTargets_Bash(t *testing.T) {
	input := json.RawMessage(`{"command":"rm -rf /tmp/project"}`)
	got := ParseTargets("Bash", input)
	if len(got.Argv) == 0 || got.Argv[0] != "rm" {
		t.Fatalf("expected argv starting with rm, got %v", got.Argv)
	}
	// /tmp/project should appear in paths
	found := false
	for _, p := range got.Paths {
		if p == "/tmp/project" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected /tmp/project in paths, got %v", got.Paths)
	}
	if got.PathMode != "delete" {
		t.Fatalf("expected path_mode delete for rm -rf, got %q", got.PathMode)
	}
}

func TestParseTargets_Write(t *testing.T) {
	input := json.RawMessage(`{"path":"/foo/bar.go","content":"package main"}`)
	got := ParseTargets("Write", input)
	if len(got.Paths) != 1 || got.Paths[0] != "/foo/bar.go" {
		t.Fatalf("expected path /foo/bar.go, got %v", got.Paths)
	}
	if got.PathMode != "write" {
		t.Fatalf("expected path_mode write, got %q", got.PathMode)
	}
}

func TestParseTargets_Edit(t *testing.T) {
	input := json.RawMessage(`{"file_path":"/src/main.go","old_string":"x","new_string":"y"}`)
	got := ParseTargets("Edit", input)
	if len(got.Paths) != 1 || got.Paths[0] != "/src/main.go" {
		t.Fatalf("expected /src/main.go, got %v", got.Paths)
	}
	if got.PathMode != "write" {
		t.Fatalf("expected path_mode write, got %q", got.PathMode)
	}
}

func TestParseTargets_Read(t *testing.T) {
	input := json.RawMessage(`{"file_path":"/etc/passwd"}`)
	got := ParseTargets("Read", input)
	if len(got.Paths) != 1 || got.Paths[0] != "/etc/passwd" {
		t.Fatalf("expected /etc/passwd, got %v", got.Paths)
	}
	if got.PathMode != "read" {
		t.Fatalf("expected path_mode read, got %q", got.PathMode)
	}
}

func TestParseTargets_Unknown(t *testing.T) {
	input := json.RawMessage(`{"whatever":"value"}`)
	got := ParseTargets("UnknownTool", input)
	// should not panic; returns empty targets
	if len(got.Argv) != 0 || len(got.Paths) != 0 {
		t.Fatalf("expected empty targets for unknown tool, got %+v", got)
	}
}

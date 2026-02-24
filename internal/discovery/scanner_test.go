package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAgentType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/home/user/.claude/projects/a/session.jsonl", want: "claude-code"},
		{path: "/home/user/.codex/sessions/2026/02/24/rollout-x.jsonl", want: "codex-cli"},
		{path: "/tmp/other/session.jsonl", want: "unknown"},
	}

	for _, tt := range tests {
		if got := detectAgentType(tt.path); got != tt.want {
			t.Fatalf("detectAgentType(%q): want %q, got %q", tt.path, tt.want, got)
		}
	}
}

func TestScanLogPaths_DetectsClaudeAndCodexSessions(t *testing.T) {
	root := t.TempDir()
	claudeFile := filepath.Join(root, ".claude", "projects", "abc", "session.jsonl")
	codexFile := filepath.Join(root, ".codex", "sessions", "2026", "02", "24", "rollout-2026-02-24T10-00-00-019c7b92-c543-7ac3-aad5-e8681852a8c5.jsonl")

	for _, path := range []string{claudeFile, codexFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	agents := ScanLogPaths([]string{filepath.Join(root, ".claude"), filepath.Join(root, ".codex")})
	if len(agents) != 2 {
		t.Fatalf("expected 2 discovered agents, got %d", len(agents))
	}

	seen := map[string]bool{}
	for _, agent := range agents {
		seen[agent.Type] = true
	}

	if !seen["claude-code"] {
		t.Fatal("expected claude-code discovery")
	}
	if !seen["codex-cli"] {
		t.Fatal("expected codex-cli discovery")
	}

	codexID := ""
	for _, agent := range agents {
		if agent.Type == "codex-cli" {
			codexID = agent.ID
			break
		}
	}

	if codexID != "codex-cli/019c7b92-c543-7ac3-aad5-e8681852a8c5" {
		t.Fatalf("expected normalized codex session ID, got %q", codexID)
	}
}

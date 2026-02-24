package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/ipc"
)

func TestStatusCommand_ShowsUnclassifiedToolCount(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "crabwise.sock")

	srv := ipc.NewServer(socketPath)
	srv.Handle("status", func(params json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"uptime":                  "1m",
			"pid":                     123,
			"agents":                  2,
			"queue_depth":             4,
			"queue_dropped":           0,
			"unclassified_tool_count": 3,
		}, nil
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("start ipc server: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Stop()
	})

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := "daemon:\n  socket_path: " + socketPath + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := captureStdout(func() error {
		cmd := newStatusCmd()
		cmd.SetArgs([]string{"--config", cfgPath})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute status command: %v", err)
	}

	if !strings.Contains(out, "Unclassified: 3") {
		t.Fatalf("expected unclassified count in output, got: %s", out)
	}
}

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crabwise-ai/crabwise/internal/ipc"
)

type testRuntimePaths struct {
	dir              string
	socketPath       string
	dbPath           string
	rawPayloadDir    string
	pidPath          string
	cfgPath          string
	logDir           string
	commandmentsPath string
}

func newTestRuntimePaths(t *testing.T) testRuntimePaths {
	t.Helper()

	dir, err := os.MkdirTemp(os.TempDir(), "cwtest-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	socketPath := filepath.Join(dir, "cw.sock")
	if len(socketPath) >= 100 {
		_ = os.RemoveAll(dir)

		shortBaseDir := "/tmp"
		if stat, statErr := os.Stat(shortBaseDir); statErr != nil || !stat.IsDir() {
			shortBaseDir = os.TempDir()
		}

		dir, err = os.MkdirTemp(shortBaseDir, "cwtest-")
		if err != nil {
			t.Fatalf("create short temp dir: %v", err)
		}
		socketPath = filepath.Join(dir, "cw.sock")
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	return testRuntimePaths{
		dir:              dir,
		socketPath:       socketPath,
		dbPath:           filepath.Join(dir, "cw.db"),
		rawPayloadDir:    filepath.Join(dir, "raw"),
		pidPath:          filepath.Join(dir, "cw.pid"),
		cfgPath:          filepath.Join(dir, "cfg.yaml"),
		logDir:           filepath.Join(dir, "logs"),
		commandmentsPath: filepath.Join(dir, "cmd.yaml"),
	}
}

func TestNewTestRuntimePaths_UsesShortPathsUnderTempDir(t *testing.T) {
	paths := newTestRuntimePaths(t)

	tempParent := filepath.Dir(paths.dir)
	tempDir := filepath.Clean(os.TempDir())
	shortTempDir := filepath.Clean("/tmp")
	if filepath.Clean(tempParent) != tempDir && filepath.Clean(tempParent) != shortTempDir {
		t.Fatalf("expected test dir under %q or %q, got %q", tempDir, shortTempDir, paths.dir)
	}
	if len(paths.socketPath) >= 100 {
		t.Fatalf("expected short socket path, got %d chars: %q", len(paths.socketPath), paths.socketPath)
	}
	if filepath.Base(paths.socketPath) != "cw.sock" {
		t.Fatalf("expected socket file name cw.sock, got %q", filepath.Base(paths.socketPath))
	}
}

func TestStatusCommand_ShowsUnclassifiedToolCount(t *testing.T) {
	paths := newTestRuntimePaths(t)

	srv := ipc.NewServer(paths.socketPath)
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

	cfg := fmt.Sprintf("daemon:\n  socket_path: %q\n", paths.socketPath)
	if err := os.WriteFile(paths.cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := captureStdout(func() error {
		cmd := newStatusCmd()
		cmd.SetArgs([]string{"--config", paths.cfgPath})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute status command: %v", err)
	}

	if !strings.Contains(out, "Unclassified: 3") {
		t.Fatalf("expected unclassified count in output, got: %s", out)
	}
}

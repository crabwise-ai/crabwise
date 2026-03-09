package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crabwise-ai/crabwise/configs"
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
)

func TestAuditTriggeredEndToEnd(t *testing.T) {
	paths := newTestRuntimePaths(t)
	if err := os.MkdirAll(paths.logDir, 0700); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}

	sessionPath := filepath.Join(paths.logDir, "session-trigger.jsonl")
	line := `{"type":"assistant","sessionId":"sess-001","cwd":"/tmp","message":{"model":"claude-sonnet-4-5-20250929","role":"assistant","content":[{"type":"tool_use","id":"toolu_001","name":"Read","input":{"file_path":"/tmp/.env"}}],"usage":{"input_tokens":100,"output_tokens":10}},"uuid":"uuid-001","timestamp":"2026-02-22T14:00:00.000Z"}`
	if err := os.WriteFile(sessionPath, []byte(line+"\n"), 0600); err != nil {
		t.Fatalf("write session log: %v", err)
	}

	if err := os.WriteFile(paths.commandmentsPath, configs.DefaultCommandmentsYAML, 0600); err != nil {
		t.Fatalf("write commandments file: %v", err)
	}

	cfgYAML := fmt.Sprintf(`daemon:
  socket_path: %q
  db_path: %q
  raw_payload_dir: %q
  pid_file: %q
discovery:
  scan_interval: 1h
  process_signatures: []
  log_paths:
    - %q
adapters:
  log_watcher:
    enabled: true
    poll_fallback_interval: 50ms
queue:
  batch_size: 1
  flush_interval: 20ms
commandments:
  file: %q
`, paths.socketPath, paths.dbPath, paths.rawPayloadDir, paths.pidPath, paths.logDir, paths.commandmentsPath)
	if err := os.WriteFile(paths.cfgPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := daemon.LoadConfig(paths.cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.New(cfg, "").Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("daemon run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("daemon did not stop within timeout")
		}
	})

	triggeredEvent, err := waitForTriggeredWarnedEvent(paths.socketPath, 10*time.Second)
	if err != nil {
		t.Fatalf("wait for triggered warned event: %v", err)
	}

	if triggeredEvent.Outcome != audit.OutcomeWarned {
		t.Fatalf("expected warned outcome, got %s", triggeredEvent.Outcome)
	}
	if triggeredEvent.CommandmentsTriggered == "" || triggeredEvent.CommandmentsTriggered == "[]" {
		t.Fatalf("expected commandments_triggered to be populated, got %q", triggeredEvent.CommandmentsTriggered)
	}
	if !strings.Contains(triggeredEvent.CommandmentsTriggered, "protect-credentials") {
		t.Fatalf("expected protect-credentials trigger, got %s", triggeredEvent.CommandmentsTriggered)
	}

	out, err := captureStdout(func() error {
		cmd := newAuditCmd()
		cmd.SetArgs([]string{"--config", paths.cfgPath, "--triggered", "--outcome", "warned", "--limit", "10"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute audit command: %v", err)
	}

	if !strings.Contains(out, "warned") {
		t.Fatalf("expected warned in audit output, got: %s", out)
	}
	if !strings.Contains(out, "file_access") {
		t.Fatalf("expected file_access in audit output, got: %s", out)
	}
}

func TestAuditCodexAgentEndToEnd(t *testing.T) {
	paths := newTestRuntimePaths(t)
	if err := os.MkdirAll(paths.logDir, 0700); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}

	sessionPath := filepath.Join(paths.logDir, "rollout-2026-02-24T10-00-00-019c7b92-c543-7ac3-aad5-e8681852a8c5.jsonl")
	line := `{"timestamp":"2026-02-24T10:00:02.000Z","type":"response_item","payload":{"type":"message","role":"assistant","model":"gpt-5.1-codex-mini","content":[{"type":"tool_call","name":"Bash","arguments":{"command":"go test ./..."}}],"usage":{"input_tokens":110,"output_tokens":9}}}`
	if err := os.WriteFile(sessionPath, []byte(line+"\n"), 0600); err != nil {
		t.Fatalf("write session log: %v", err)
	}

	if err := os.WriteFile(paths.commandmentsPath, configs.DefaultCommandmentsYAML, 0600); err != nil {
		t.Fatalf("write commandments file: %v", err)
	}

	cfgYAML := fmt.Sprintf(`daemon:
  socket_path: %q
  db_path: %q
  raw_payload_dir: %q
  pid_file: %q
discovery:
  scan_interval: 1h
  process_signatures: []
  log_paths:
    - %q
adapters:
  log_watcher:
    enabled: true
    poll_fallback_interval: 50ms
queue:
  batch_size: 1
  flush_interval: 20ms
commandments:
  file: %q
`, paths.socketPath, paths.dbPath, paths.rawPayloadDir, paths.pidPath, paths.logDir, paths.commandmentsPath)
	if err := os.WriteFile(paths.cfgPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := daemon.LoadConfig(paths.cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.New(cfg, "").Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("daemon run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("daemon did not stop within timeout")
		}
	})

	evt, err := waitForAgentEvent(paths.socketPath, "codex-cli", 10*time.Second)
	if err != nil {
		t.Fatalf("wait for codex event: %v", err)
	}

	if evt.AgentID != "codex-cli" {
		t.Fatalf("expected codex-cli agent, got %s", evt.AgentID)
	}
	if evt.ActionType != audit.ActionCommandExecution {
		t.Fatalf("expected command_execution, got %s", evt.ActionType)
	}
	if evt.Model != "gpt-5.1-codex-mini" {
		t.Fatalf("expected codex model, got %s", evt.Model)
	}

	out, err := captureStdout(func() error {
		cmd := newAuditCmd()
		cmd.SetArgs([]string{"--config", paths.cfgPath, "--agent", "codex-cli", "--limit", "10"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("execute audit command: %v", err)
	}

	if !strings.Contains(out, "codex-cli") {
		t.Fatalf("expected codex-cli in output, got: %s", out)
	}
	if !strings.Contains(out, "command_execution") {
		t.Fatalf("expected command_execution in output, got: %s", out)
	}
}

func waitForTriggeredWarnedEvent(socketPath string, timeout time.Duration) (*audit.AuditEvent, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		result, callErr := client.Call("audit.query", map[string]interface{}{
			"triggered": true,
			"outcome":   "warned",
			"limit":     10,
		})
		_ = client.Close()
		if callErr != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		var qr audit.QueryResult
		if err := json.Unmarshal(result, &qr); err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if len(qr.Events) > 0 {
			return qr.Events[0], nil
		}

		time.Sleep(50 * time.Millisecond)
	}

	return nil, fmt.Errorf("timed out waiting for triggered warned event")
}

func waitForAgentEvent(socketPath, agent string, timeout time.Duration) (*audit.AuditEvent, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		client, err := ipc.Dial(socketPath)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		result, callErr := client.Call("audit.query", map[string]interface{}{
			"agent": agent,
			"limit": 10,
		})
		_ = client.Close()
		if callErr != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		var qr audit.QueryResult
		if err := json.Unmarshal(result, &qr); err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if len(qr.Events) > 0 {
			return qr.Events[0], nil
		}

		time.Sleep(50 * time.Millisecond)
	}

	return nil, fmt.Errorf("timed out waiting for agent %s event", agent)
}

func captureStdout(run func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}

	os.Stdout = w
	runErr := run()
	_ = w.Close()
	os.Stdout = oldStdout

	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		return "", readErr
	}

	return string(out), runErr
}

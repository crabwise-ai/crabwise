package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerClientRoundTrip(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	srv := NewServer(socketPath)
	srv.Handle("status", func(params json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"uptime":  "10s",
			"agents":  2,
			"version": "test",
		}, nil
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer srv.Stop()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	result, err := client.Call("status", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	var status map[string]interface{}
	if err := json.Unmarshal(result, &status); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if status["version"] != "test" {
		t.Fatalf("expected version=test, got %v", status["version"])
	}
}

func TestServerMethodNotFound(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	srv := NewServer(socketPath)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	_, err = client.Call("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
}

func TestServerSocketPermissions(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	srv := NewServer(socketPath)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	// Check directory permissions
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	_ = dirInfo // temp dir permissions vary, just check socket

	// Check socket permissions
	sockInfo, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	perm := sockInfo.Mode().Perm()
	if perm != 0600 {
		t.Fatalf("expected socket perm 0600, got %o", perm)
	}
}

func TestServerSubscribe(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	srv := NewServer(socketPath)
	srv.HandleSubscribe(func(params json.RawMessage, send func(string, interface{}) error, done <-chan struct{}) error {
		// Send a couple test events
		send("audit.event", map[string]string{"id": "evt_001"})
		send("audit.event", map[string]string{"id": "evt_002"})
		<-done
		return nil
	})

	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	scanner, err := client.Subscribe("audit.subscribe", nil)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Read 2 notifications
	for i := 0; i < 2; i++ {
		done := make(chan bool, 1)
		go func() {
			if scanner.Scan() {
				done <- true
			} else {
				done <- false
			}
		}()

		select {
		case ok := <-done:
			if !ok {
				t.Fatal("scanner failed")
			}
			var notif Notification
			json.Unmarshal(scanner.Bytes(), &notif)
			if notif.Method != "audit.event" {
				t.Fatalf("expected audit.event, got %s", notif.Method)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout reading notification")
		}
	}

	client.Close()
}

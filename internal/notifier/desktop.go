package notifier

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

// DesktopBackend sends OS desktop notifications for blocked events.
type DesktopBackend struct {
	minInterval time.Duration
	mu          sync.Mutex
	lastSent    time.Time
	warnedOnce  bool
}

// NewDesktopBackend creates a desktop notification backend.
func NewDesktopBackend(cfg DesktopConfig) *DesktopBackend {
	return &DesktopBackend{
		minInterval: cfg.MinInterval,
	}
}

func (d *DesktopBackend) Name() string { return "desktop" }

func (d *DesktopBackend) Send(ctx context.Context, evt *audit.AuditEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.minInterval > 0 && time.Since(d.lastSent) < d.minInterval {
		return nil
	}

	title := "Crabwise: Agent Blocked"
	body := fmt.Sprintf("Action %q blocked for agent %s", evt.Action, evt.AgentID)

	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.CommandContext(ctx, "notify-send", "-u", "critical", title, body).Run()
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title %q`, body, title)
		err = exec.CommandContext(ctx, "osascript", "-e", script).Run()
	default:
		err = fmt.Errorf("unsupported OS %q", runtime.GOOS)
	}

	if err != nil {
		if !d.warnedOnce {
			log.Printf("notifier: desktop notification failed (will fallback to bell): %v", err)
			d.warnedOnce = true
		}
		// Bell fallback
		fmt.Fprint(os.Stderr, "\a")
		return nil
	}

	d.lastSent = time.Now()
	return nil
}

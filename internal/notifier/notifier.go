package notifier

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
)

// Backend sends a block notification.
type Backend interface {
	Name() string
	Send(ctx context.Context, evt *audit.AuditEvent) error
}

// Config mirrors daemon.NotificationsConfig to avoid import cycle.
type Config struct {
	Desktop DesktopConfig
	Webhook WebhookConfig
}

type DesktopConfig struct {
	Enabled     bool
	MinInterval time.Duration
}

type WebhookConfig struct {
	Enabled       bool
	URL           string
	AuthHeaderEnv string
	MinInterval   time.Duration
}

// Notifier subscribes to audit events and dispatches blocked-only alerts.
type Notifier struct {
	mu       sync.Mutex
	backends []Backend
	logger   *audit.Logger
	cancel   context.CancelFunc
	done     chan struct{}
}

// New creates a Notifier with backends built from cfg.
func New(logger *audit.Logger, cfg Config) *Notifier {
	var backends []Backend
	if cfg.Desktop.Enabled {
		backends = append(backends, NewDesktopBackend(cfg.Desktop))
	}
	if cfg.Webhook.Enabled {
		backends = append(backends, NewWebhookBackend(cfg.Webhook))
	}
	return &Notifier{
		logger:   logger,
		backends: backends,
	}
}

// Start subscribes to audit events and dispatches to backends in a goroutine.
func (n *Notifier) Start(ctx context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()

	ctx, n.cancel = context.WithCancel(ctx)
	n.done = make(chan struct{})
	ch := n.logger.Subscribe()

	go func() {
		defer close(n.done)
		defer n.logger.Unsubscribe(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if evt.Outcome != audit.OutcomeBlocked {
					continue
				}
				for _, b := range n.backends {
					if err := b.Send(ctx, evt); err != nil {
						log.Printf("notifier: %s send error: %v", b.Name(), err)
					}
				}
			}
		}
	}()
}

// Stop cancels the goroutine and waits for exit.
func (n *Notifier) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
	if n.done != nil {
		<-n.done
		n.done = nil
	}
}

// Reload stops the notifier, rebuilds backends from new config, and restarts.
func (n *Notifier) Reload(ctx context.Context, cfg Config) {
	n.Stop()

	n.mu.Lock()
	var backends []Backend
	if cfg.Desktop.Enabled {
		backends = append(backends, NewDesktopBackend(cfg.Desktop))
	}
	if cfg.Webhook.Enabled {
		backends = append(backends, NewWebhookBackend(cfg.Webhook))
	}
	n.backends = backends
	n.mu.Unlock()

	n.Start(ctx)
}

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crabwise-ai/crabwise/internal/adapter/logwatcher"
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/discovery"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/queue"
	"github.com/crabwise-ai/crabwise/internal/store"
)

type Daemon struct {
	cfg          *Config
	store        *store.Store
	queue        *queue.Queue
	logger       *audit.Logger
	ipcServer    *ipc.Server
	registry     *discovery.Registry
	watcher      *logwatcher.LogWatcher
	commandments CommandmentsService
	startTime    time.Time

	hostname string
	userID   string

	eventCh chan *audit.AuditEvent
	cancel  context.CancelFunc
}

func New(cfg *Config) *Daemon {
	return &Daemon{
		cfg:      cfg,
		registry: discovery.NewRegistry(),
		eventCh:  make(chan *audit.AuditEvent, 1000),
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	ctx, d.cancel = context.WithCancel(ctx)
	d.startTime = time.Now()

	// Resolve origin identity once at startup — UID is kernel-verified, not spoofable
	d.hostname, _ = os.Hostname()
	d.userID = strconv.Itoa(os.Getuid())

	// PID file
	if err := d.writePID(); err != nil {
		return fmt.Errorf("pid file: %w", err)
	}
	defer d.removePID()

	// SQLite
	s, err := store.Open(d.cfg.Daemon.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	d.store = s
	defer s.Close()

	// Queue
	policy := queue.PolicyBlockWithTimeout
	if d.cfg.Queue.Overflow == "drop_oldest" {
		policy = queue.PolicyDropOldest
	}
	d.queue = queue.New(d.cfg.Queue.Capacity, policy, d.cfg.Queue.BlockTimeout.Duration())

	// Audit logger
	d.logger, err = audit.NewLogger(s, d.queue, d.cfg.Queue.BatchSize, d.cfg.Queue.FlushInterval.Duration())
	if err != nil {
		return fmt.Errorf("audit logger: %w", err)
	}

	var commandmentsInitErr error
	if d.commandments, commandmentsInitErr = NewCommandmentsService(d.cfg.Commandments.File, DefaultCommandmentsYAML); commandmentsInitErr != nil {
		log.Printf("daemon: commandments init error: %v", commandmentsInitErr)
		d.commandments = &noopCommandmentsService{}
	}
	d.logger.SetEvaluator(d.commandments)
	d.logger.SetRedactor(d.commandments)

	d.logger.Start(ctx)
	defer d.logger.Stop()

	if commandmentsInitErr != nil {
		d.emitSystemEvent("commandments_load_failed", audit.OutcomeFailure, map[string]interface{}{"error": commandmentsInitErr.Error()})
	} else {
		rulesLoaded := len(d.commandments.List())
		d.emitSystemEvent("commandments_load_ok", audit.OutcomeSuccess, map[string]interface{}{"rules_loaded": rulesLoaded})
	}

	// IPC server
	d.ipcServer = ipc.NewServer(d.cfg.Daemon.SocketPath)
	d.registerIPC()
	if err := d.ipcServer.Start(); err != nil {
		return fmt.Errorf("ipc server: %w", err)
	}
	defer func() {
		if err := d.ipcServer.Stop(); err != nil {
			log.Printf("daemon: ipc server stop: %v", err)
		}
	}()

	// Log watcher adapter
	d.watcher = logwatcher.New(
		d.cfg.Discovery.LogPaths,
		d.cfg.Adapters.LogWatcher.PollFallbackInterval.Duration(),
		d.store,
	)
	if d.cfg.Adapters.LogWatcher.Enabled {
		if err := d.watcher.Start(ctx, d.eventCh); err != nil {
			log.Printf("daemon: log watcher start error: %v", err)
		}
		defer func() {
			if err := d.watcher.Stop(); err != nil {
				log.Printf("daemon: watcher stop: %v", err)
			}
		}()
	}

	// Event forwarder: eventCh → queue
	go d.forwardEvents(ctx)

	// Discovery scanner
	go d.discoveryLoop(ctx)

	log.Printf("daemon: started (pid=%d, socket=%s, db=%s)", os.Getpid(), d.cfg.Daemon.SocketPath, d.cfg.Daemon.DBPath)

	// Wait for signal loop
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				if _, err := d.reloadCommandments(); err != nil {
					log.Printf("daemon: commandments reload error: %v", err)
				}
			default:
				log.Printf("daemon: received %s, shutting down", sig)
				d.cancel()
				return nil
			}
		case <-ctx.Done():
			log.Printf("daemon: context cancelled, shutting down")
			d.cancel()
			return nil
		}
	}
}

func (d *Daemon) forwardEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-d.eventCh:
			if !ok {
				return
			}
			evt.Hostname = d.hostname
			evt.UserID = d.userID
			d.queue.Send(evt)
		}
	}
}

func (d *Daemon) discoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.Discovery.ScanInterval.Duration())
	defer ticker.Stop()

	scan := func() {
		var agents []discovery.AgentInfo
		agents = append(agents, discovery.ScanProcesses(d.cfg.Discovery.ProcessSignatures)...)
		agents = append(agents, discovery.ScanLogPaths(d.cfg.Discovery.LogPaths)...)
		d.registry.Update(agents)
	}

	scan() // initial scan
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scan()
		}
	}
}

func (d *Daemon) registerIPC() {
	d.ipcServer.Handle("status", func(params json.RawMessage) (interface{}, error) {
		stats := d.queue.Stats()
		return map[string]interface{}{
			"uptime":        time.Since(d.startTime).Truncate(time.Second).String(),
			"agents":        d.registry.Count(),
			"queue_depth":   stats.Depth,
			"queue_dropped": stats.Dropped,
			"pid":           os.Getpid(),
		}, nil
	})

	d.ipcServer.Handle("agents.list", func(params json.RawMessage) (interface{}, error) {
		return d.registry.List(), nil
	})

	d.ipcServer.Handle("audit.query", func(params json.RawMessage) (interface{}, error) {
		var filter audit.QueryFilter
		if len(params) > 0 {
			var f struct {
				Since     string `json:"since"`
				Until     string `json:"until"`
				Agent     string `json:"agent"`
				Action    string `json:"action"`
				Session   string `json:"session"`
				Outcome   string `json:"outcome"`
				Triggered bool   `json:"triggered"`
				Limit     int    `json:"limit"`
				Offset    int    `json:"offset"`
			}
			if err := json.Unmarshal(params, &f); err != nil {
				return nil, fmt.Errorf("parse params: %w", err)
			}
			if f.Since != "" {
				t, _ := time.Parse(time.RFC3339, f.Since)
				filter.Since = &t
			}
			if f.Until != "" {
				t, _ := time.Parse(time.RFC3339, f.Until)
				filter.Until = &t
			}
			filter.Agent = f.Agent
			filter.Action = f.Action
			filter.Session = f.Session
			filter.Outcome = f.Outcome
			filter.TriggeredOnly = f.Triggered
			filter.Limit = f.Limit
			filter.Offset = f.Offset
		}
		return audit.QueryEvents(d.store.DB(), filter)
	})

	d.ipcServer.Handle("commandments.list", func(params json.RawMessage) (interface{}, error) {
		if d.commandments == nil {
			return []CommandmentRuleSummary{}, nil
		}
		return d.commandments.List(), nil
	})

	d.ipcServer.Handle("commandments.test", func(params json.RawMessage) (interface{}, error) {
		if d.commandments == nil {
			return audit.EvalResult{Evaluated: []string{}, Triggered: []audit.TriggeredRule{}}, nil
		}

		if len(params) == 0 {
			return nil, fmt.Errorf("missing params")
		}

		var event audit.AuditEvent
		var payload struct {
			Event json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(params, &payload); err == nil && len(payload.Event) > 0 {
			if err := json.Unmarshal(payload.Event, &event); err != nil {
				return nil, fmt.Errorf("parse event: %w", err)
			}
		} else {
			if err := json.Unmarshal(params, &event); err != nil {
				return nil, fmt.Errorf("parse event: %w", err)
			}
		}

		return d.commandments.Test(&event), nil
	})

	d.ipcServer.Handle("commandments.reload", func(params json.RawMessage) (interface{}, error) {
		rulesLoaded, err := d.reloadCommandments()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "rules_loaded": rulesLoaded}, nil
	})

	d.ipcServer.Handle("audit.verify", func(params json.RawMessage) (interface{}, error) {
		valid, total, brokenAt, err := audit.VerifyIntegrity(d.store.DB())
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"valid":     valid,
			"total":     total,
			"broken_at": brokenAt,
		}, nil
	})

	d.ipcServer.Handle("audit.export", func(params json.RawMessage) (interface{}, error) {
		result, err := audit.QueryEvents(d.store.DB(), audit.QueryFilter{})
		if err != nil {
			return nil, err
		}
		return result.Events, nil
	})

	d.ipcServer.HandleSubscribe(func(params json.RawMessage, send func(string, interface{}) error, done <-chan struct{}) error {
		ch := d.logger.Subscribe()
		defer d.logger.Unsubscribe(ch)

		for {
			select {
			case <-done:
				return nil
			case evt, ok := <-ch:
				if !ok {
					return nil
				}
				if err := send("audit.event", evt); err != nil {
					return err
				}
			}
		}
	})
}

func (d *Daemon) writePID() error {
	dir := filepath.Dir(d.cfg.Daemon.PIDFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Check if already running
	if data, err := os.ReadFile(d.cfg.Daemon.PIDFile); err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pid, err := strconv.Atoi(pidStr); err == nil {
			if process, err := os.FindProcess(pid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					return fmt.Errorf("daemon already running (pid %d)", pid)
				}
			}
		}
	}

	return os.WriteFile(d.cfg.Daemon.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0600)
}

func (d *Daemon) removePID() {
	os.Remove(d.cfg.Daemon.PIDFile)
}

func (d *Daemon) reloadCommandments() (int, error) {
	if d.commandments == nil {
		d.commandments = &noopCommandmentsService{}
	}

	rulesLoaded, err := d.commandments.Reload()
	if err != nil {
		d.emitSystemEvent("commandments_reload_failed", audit.OutcomeFailure, map[string]interface{}{"error": err.Error()})
		return 0, err
	}

	if rulesLoaded == 0 {
		rulesLoaded = len(d.commandments.List())
	}
	d.emitSystemEvent("commandments_reload_ok", audit.OutcomeSuccess, map[string]interface{}{"rules_loaded": rulesLoaded})
	return rulesLoaded, nil
}

func (d *Daemon) emitSystemEvent(action string, outcome audit.Outcome, payload interface{}) {
	if d.queue == nil {
		return
	}

	args := ""
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			args = string(b)
		}
	}

	now := time.Now().UTC()
	e := &audit.AuditEvent{
		ID:          fmt.Sprintf("sys_%d_%s", now.UnixNano(), action),
		Timestamp:   now,
		AgentID:     "crabwise",
		ActionType:  audit.ActionSystem,
		Action:      action,
		Arguments:   args,
		Outcome:     outcome,
		AdapterID:   "daemon",
		AdapterType: "daemon",
		Hostname:    d.hostname,
		UserID:      d.userID,
	}

	d.queue.Send(e)
}

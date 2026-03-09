package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/spf13/cobra"
)

type watchRunner func(cfg *daemon.Config, outcome string) error

var runWatchTextMode watchRunner = runWatchText
var runWatchTUIMode watchRunner = runWatchTUI

func newWatchCmd() *cobra.Command {
	var configPath string
	var textMode bool
	var outcome string

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream live events",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if textMode {
				return runWatchTextMode(cfg, outcome)
			}

			return runWatchTUIMode(cfg, outcome)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().BoolVar(&textMode, "text", false, "use plain-text stream output")
	cmd.Flags().StringVar(&outcome, "outcome", "", "filter events by outcome (e.g. blocked)")
	return cmd
}

func runWatchText(cfg *daemon.Config, outcome string) error {
	client, err := ipc.Dial(cfg.Daemon.SocketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer client.Close()

	var subscribeParams json.RawMessage
	if outcome != "" {
		subscribeParams, _ = json.Marshal(map[string]string{"outcome": outcome})
	}
	scanner, err := client.Subscribe("audit.subscribe", subscribeParams)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	fmt.Println("Watching for events... (Ctrl+C to stop)")
	var intentionalClose atomic.Bool

	go func() {
		<-sigCh
		intentionalClose.Store(true)
		_ = client.Close()
	}()

	for scanner.Scan() {
		var notif struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &notif); err != nil {
			continue
		}

		switch notif.Method {
		case "audit.event":
			var evt audit.AuditEvent
			if err := json.Unmarshal(notif.Params, &evt); err != nil {
				continue
			}
			fmt.Println(formatWatchTextEvent(evt))
		case "audit.heartbeat":
			// silent
		}
	}

	return watchTextExitErr(intentionalClose.Load(), scanner.Err())
}

func watchTextExitErr(interrupted bool, err error) error {
	if !interrupted {
		return err
	}
	if err == nil || errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrClosed) {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "use of closed network connection") || strings.Contains(msg, "file already closed") {
		return nil
	}
	return err
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatWatchTextEvent(evt audit.AuditEvent) string {
	ts := evt.Timestamp.Format("15:04:05")
	return fmt.Sprintf("%s [%s] %-18s %-10s %s",
		ts,
		fullAgentLabel(evt.AgentID, evt.SessionID),
		evt.ActionType,
		evt.Action,
		truncate(evt.Arguments, 60),
	)
}

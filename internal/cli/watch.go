package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream live events",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			client, err := ipc.Dial(cfg.Daemon.SocketPath)
			if err != nil {
				return fmt.Errorf("connect to daemon: %w", err)
			}
			defer client.Close()

			scanner, err := client.Subscribe("audit.subscribe", nil)
			if err != nil {
				return fmt.Errorf("subscribe: %w", err)
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			fmt.Println("Watching for events... (Ctrl+C to stop)")

			go func() {
				<-sigCh
				client.Close()
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
					ts := evt.Timestamp.Format("15:04:05")
					fmt.Printf("%s [%s] %-18s %-10s %s\n",
						ts, evt.AgentID, evt.ActionType, evt.Action, truncate(evt.Arguments, 60))
				case "audit.heartbeat":
					// silent
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/discovery"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

func newAgentsCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List discovered agents",
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

			result, err := client.Call("agents.list", nil)
			if err != nil {
				return fmt.Errorf("agents.list: %w", err)
			}

			var agents []discovery.AgentInfo
			if err := json.Unmarshal(result, &agents); err != nil {
				return fmt.Errorf("parse agents: %w", err)
			}

			if len(agents) == 0 {
				fmt.Println("No agents discovered.")
				return nil
			}

			if isPlain() {
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "ID\tTYPE\tPID\tSTATUS")
				for _, a := range agents {
					fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", a.ID, a.Type, a.PID, a.Status)
				}
				w.Flush()
			} else {
				fmt.Printf("  %s  %s  %s  %s\n",
					tui.StyleHeading.Render(fmt.Sprintf("%-20s", "ID")),
					tui.StyleHeading.Render(fmt.Sprintf("%-12s", "TYPE")),
					tui.StyleHeading.Render(fmt.Sprintf("%-8s", "PID")),
					tui.StyleHeading.Render("STATUS"),
				)
				for _, a := range agents {
					fmt.Printf("  %s %s  %s  %s  %s\n",
						tui.StatusIcon(a.Status),
						tui.StyleBody.Render(fmt.Sprintf("%-20s", a.ID)),
						tui.StyleMuted.Render(fmt.Sprintf("%-12s", a.Type)),
						tui.StyleMuted.Render(fmt.Sprintf("%-8d", a.PID)),
						tui.StyleBody.Render(a.Status),
					)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			client, err := ipc.Dial(cfg.Daemon.SocketPath)
			if err != nil {
				if isPlain() {
					fmt.Println("Daemon is not running.")
				} else {
					fmt.Printf("  %s %s\n", tui.StatusIcon("stopped"), tui.StyleBody.Render("Daemon not running"))
				}
				return nil
			}
			defer client.Close()

			result, err := client.Call("status", nil)
			if err != nil {
				return fmt.Errorf("status call: %w", err)
			}

			var status map[string]interface{}
			if err := json.Unmarshal(result, &status); err != nil {
				return fmt.Errorf("parse status: %w", err)
			}

			if isPlain() {
				fmt.Printf("Status:       running\n")
				fmt.Printf("Uptime:       %v\n", status["uptime"])
				fmt.Printf("PID:          %v\n", status["pid"])
				fmt.Printf("Agents:       %v\n", status["agents"])
				fmt.Printf("Queue depth:  %v\n", status["queue_depth"])
				fmt.Printf("Dropped:      %v\n", status["queue_dropped"])
				fmt.Printf("Unclassified: %v\n", status["unclassified_tool_count"])
				if _, ok := status["proxy_requests_total"]; ok {
					fmt.Printf("Proxy reqs:   %v\n", status["proxy_requests_total"])
					fmt.Printf("Proxy blocked:%v\n", status["proxy_blocked_total"])
					fmt.Printf("Proxy errors: %v\n", status["proxy_upstream_errors"])
					fmt.Printf("Map degraded: %v\n", status["mapping_degraded_count"])
				}
			} else {
				fmt.Printf("  %s %s\n", tui.StatusIcon("running"), tui.StyleBody.Render("Daemon running"))
				printStatusLine("Uptime", status["uptime"])
				printStatusLine("PID", status["pid"])
				printStatusLine("Agents", status["agents"])
				printStatusLine("Queue depth", status["queue_depth"])
				printStatusLine("Dropped", status["queue_dropped"])
				printStatusLine("Unclassified", status["unclassified_tool_count"])
				if _, ok := status["proxy_requests_total"]; ok {
					printStatusLine("Proxy reqs", status["proxy_requests_total"])
					printStatusLine("Proxy blocked", status["proxy_blocked_total"])
					printStatusLine("Proxy errors", status["proxy_upstream_errors"])
					printStatusLine("Map degraded", status["mapping_degraded_count"])
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

func printStatusLine(label string, value interface{}) {
	fmt.Printf("  %s %s\n", tui.StyleMuted.Render(label+":"), tui.StyleBody.Render(fmt.Sprintf("%v", value)))
}

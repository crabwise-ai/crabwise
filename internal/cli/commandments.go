package cli

import (
	"encoding/json"
	"fmt"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/ipc"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

func newCommandmentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commandments",
		Short: "Manage and inspect commandments",
	}

	cmd.AddCommand(
		newCommandmentsListCmd(),
		newCommandmentsTestCmd(),
		newCommandmentsReloadCmd(),
	)

	return cmd
}

func newCommandmentsListCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active commandments",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isPlain() {
				cfg, err := daemon.LoadConfig(configPath)
				if err != nil {
					return fmt.Errorf("load config: %w", err)
				}
				return runCommandmentsTUI(cfg.Daemon.SocketPath)
			}

			return commandmentsListPlain(configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

func commandmentsListPlain(configPath string) error {
	client, err := dialCommandmentsClient(configPath)
	if err != nil {
		return err
	}
	defer client.Close()

	result, err := client.Call("commandments.list", nil)
	if err != nil {
		return fmt.Errorf("commandments.list: %w", err)
	}

	var rules []daemon.CommandmentRuleSummary
	if err := json.Unmarshal(result, &rules); err != nil {
		return fmt.Errorf("parse result: %w", err)
	}

	if len(rules) == 0 {
		fmt.Println("No commandments loaded.")
		return nil
	}

	fmt.Printf("%-32s %-11s %-8s %s\n", "NAME", "ENFORCEMENT", "PRIORITY", "ENABLED")
	for _, rule := range rules {
		fmt.Printf("%-32s %-11s %-8d %t\n", rule.Name, rule.Enforcement, rule.Priority, rule.Enabled)
	}
	return nil
}

func newCommandmentsTestCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "test <event-json>",
		Short: "Dry-run commandments against an event JSON payload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialCommandmentsClient(configPath)
			if err != nil {
				return err
			}
			defer client.Close()

			result, err := client.Call("commandments.test", map[string]interface{}{
				"event": json.RawMessage(args[0]),
			})
			if err != nil {
				return fmt.Errorf("commandments.test: %w", err)
			}

			var eval audit.EvalResult
			if err := json.Unmarshal(result, &eval); err != nil {
				return fmt.Errorf("parse result: %w", err)
			}

			if isPlain() {
				fmt.Printf("Evaluated: %v\n", eval.Evaluated)
				if len(eval.Triggered) == 0 {
					fmt.Println("Triggered: []")
					return nil
				}
				fmt.Println("Triggered:")
				for _, tr := range eval.Triggered {
					fmt.Printf("- %s (%s)", tr.Name, tr.Enforcement)
					if tr.Message != "" {
						fmt.Printf(": %s", tr.Message)
					}
					fmt.Println()
				}
			} else {
				fmt.Printf("  %s %s\n", tui.StyleMuted.Render("Evaluated:"), tui.StyleBody.Render(fmt.Sprintf("%v", eval.Evaluated)))
				if len(eval.Triggered) == 0 {
					fmt.Printf("  %s %s\n", tui.StyleSuccess.Render("✓"), tui.StyleBody.Render("No blocks triggered"))
					return nil
				}
				fmt.Printf("  %s\n", tui.StyleMuted.Render("Triggered:"))
				for _, tr := range eval.Triggered {
					var icon string
					switch tr.Enforcement {
					case "block":
						icon = tui.StyleError.Render("✖")
					default:
						icon = tui.StyleWarning.Render("⚠")
					}
					line := fmt.Sprintf("  %s %s (%s)", icon, tui.StyleBody.Bold(true).Render(tr.Name), tr.Enforcement)
					if tr.Message != "" {
						line += ": " + tui.StyleMuted.Render(tr.Message)
					}
					fmt.Println(line)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

func newCommandmentsReloadCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "reload",
		Short: "Reload commandments file in the running daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialCommandmentsClient(configPath)
			if err != nil {
				return err
			}
			defer client.Close()

			result, err := client.Call("commandments.reload", nil)
			if err != nil {
				return fmt.Errorf("commandments.reload: %w", err)
			}

			var out struct {
				OK          bool `json:"ok"`
				RulesLoaded int  `json:"rules_loaded"`
			}
			if err := json.Unmarshal(result, &out); err != nil {
				return fmt.Errorf("parse result: %w", err)
			}

			if isPlain() {
				fmt.Printf("Reloaded commandments (%d rules).\n", out.RulesLoaded)
			} else {
				fmt.Printf("  %s %s\n", tui.StyleSuccess.Render("✓"), tui.StyleBody.Render(fmt.Sprintf("Loaded %d rules", out.RulesLoaded)))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

func dialCommandmentsClient(configPath string) (*ipc.Client, error) {
	cfg, err := daemon.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	client, err := ipc.Dial(cfg.Daemon.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}

	return client, nil
}

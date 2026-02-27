package cli

import (
	"context"
	"fmt"
	"log"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

func newStartCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the crabwise daemon (foreground)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if !isPlain() {
				fmt.Println(tui.RenderBannerStatic(Version))
				fmt.Println()
				fmt.Printf("  %s %s\n", tui.StatusIcon("connecting"), tui.StyleBody.Render("Starting daemon..."))
				fmt.Printf("  %s %s\n", tui.StatusIcon("connecting"), tui.StyleBody.Render("Initializing log watcher..."))
				if cfg.Adapters.Proxy.Listen != "" {
					fmt.Printf("  %s %s\n", tui.StatusIcon("connecting"), tui.StyleBody.Render("Configuring proxy on "+cfg.Adapters.Proxy.Listen))
				}
				fmt.Println()
			}

			log.SetFlags(log.Ltime | log.Lshortfile)
			daemon.Version = Version
			d := daemon.New(cfg)
			return d.Run(context.Background())
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

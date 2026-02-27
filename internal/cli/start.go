package cli

import (
	"context"
	"fmt"
	"log"

	"github.com/crabwise-ai/crabwise/internal/certs"
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

			if cfg.Adapters.Proxy.Enabled {
				status := certs.CheckTrust(cfg.Adapters.Proxy.CACert, cfg.Adapters.Proxy.CAKey)
				if !status.Exists {
					if isPlain() {
						fmt.Printf("WARNING: Crabwise proxy is enabled but CA files are missing.\n")
						fmt.Printf("Run: crabwise cert trust\n\n")
					} else {
						body := "Crabwise proxy is enabled but the CA cert/key files are missing.\n\n" +
							"Run:\ncrabwise cert trust"
						fmt.Println(tui.RenderPanel("Action required: Generate the CA", body))
						fmt.Println()
					}
				} else if !status.Trusted {
					commands := certs.CommandsForOS(cfg.Adapters.Proxy.CACert)
					if isPlain() {
						fmt.Printf("WARNING: Crabwise CA is not trusted (%s).\n", status.Reason)
						if commands.SystemTrustCmd != "" {
							fmt.Printf("To trust it, run:\n%s\n", commands.SystemTrustCmd)
							fmt.Printf("Or: crabwise cert trust --copy\n")
						} else {
							fmt.Printf("Manually add the CA to your system trust store:\n%s\n", status.CertPath)
						}
						fmt.Println()
						} else if commands.SystemTrustCmd != "" {
						body := "Crabwise is configured to intercept HTTPS (MITM).\n\n" +
							"Copy/paste:\n" + commands.SystemTrustCmd + "\n\n" +
							"Or copy it:\ncrabwise cert trust --copy"
						fmt.Println(tui.RenderPanel("Action required: Trust the CA ("+commands.OS+")", body))
						fmt.Println()
						} else {
						body := "Crabwise is configured to intercept HTTPS (MITM).\n\n" +
							"Manually add this CA to your system trust store:\n" + status.CertPath
						fmt.Println(tui.RenderPanel("Action required: Trust the CA", body))
						fmt.Println()
						}
				}
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

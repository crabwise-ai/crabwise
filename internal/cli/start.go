package cli

import (
	"context"
	"fmt"
	"log"

	"github.com/crabwise-ai/crabwise/internal/certs"
	"github.com/crabwise-ai/crabwise/internal/daemon"
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
				return runStartTUI(cfg)
			}

			if cfg.Adapters.Proxy.Enabled {
				status := certs.CheckTrust(cfg.Adapters.Proxy.CACert, cfg.Adapters.Proxy.CAKey)
				if !status.Exists {
					fmt.Printf("WARNING: Crabwise proxy is enabled but CA files are missing.\n")
					fmt.Printf("Run: crabwise cert trust\n\n")
				} else if !status.Trusted {
					commands := certs.CommandsForOS(cfg.Adapters.Proxy.CACert)
					fmt.Printf("WARNING: Crabwise CA is not trusted (%s).\n", status.Reason)
					if commands.SystemTrustCmd != "" {
						fmt.Printf("To trust it, run:\n%s\n", commands.SystemTrustCmd)
						fmt.Printf("Or: crabwise cert trust --copy\n")
					} else {
						fmt.Printf("Manually add the CA to your system trust store:\n%s\n", status.CertPath)
					}
					fmt.Println()
				}
			}

			log.SetFlags(log.Ltime | log.Lshortfile)
			daemon.Version = Version
			d := daemon.New(cfg, configPath)
			return d.Run(context.Background())
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

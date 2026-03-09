package cli

import (
	"fmt"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/spf13/cobra"
)

func newSettingsCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "settings",
		Short: "View and edit crabwise configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = daemon.DefaultConfigPath()
			}

			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			return runSettingsTUI(cfg, configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

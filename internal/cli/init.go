package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crabwise-ai/crabwise/configs"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize default config file",
		Long:  "Write the default configuration to ~/.config/crabwise/config.yaml. Does not overwrite existing config unless --force is used.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir := defaultInitConfigDir()
			configPath := filepath.Join(configDir, "config.yaml")

			if _, err := os.Stat(configPath); err == nil && !force {
				return fmt.Errorf("config already exists at %s (use --force to overwrite)", configPath)
			}

			if err := os.MkdirAll(configDir, 0700); err != nil {
				return fmt.Errorf("create config dir: %w", err)
			}

			if err := os.WriteFile(configPath, configs.DefaultYAML, 0600); err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			fmt.Printf("Config written to %s\n", configPath)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config")
	return cmd
}

func defaultInitConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "crabwise")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "crabwise")
}

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
		Long:  "Write default configuration files to ~/.config/crabwise/. Does not overwrite existing files unless --force is used.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir := defaultInitConfigDir()
			configPath := filepath.Join(configDir, "config.yaml")
			commandmentsPath := filepath.Join(configDir, "commandments.yaml")
			toolRegistryPath := filepath.Join(configDir, "tool_registry.yaml")

			if err := os.MkdirAll(configDir, 0700); err != nil {
				return fmt.Errorf("create config dir: %w", err)
			}

			configWritten, err := writeDefaultFile(configPath, configs.DefaultYAML, force)
			if err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			commandmentsWritten, err := writeDefaultFile(commandmentsPath, configs.DefaultCommandmentsYAML, force)
			if err != nil {
				return fmt.Errorf("write commandments: %w", err)
			}

			toolRegistryWritten, err := writeDefaultFile(toolRegistryPath, configs.DefaultToolRegistryYAML, force)
			if err != nil {
				return fmt.Errorf("write tool registry: %w", err)
			}

			if configWritten {
				fmt.Printf("Config written to %s\n", configPath)
			} else {
				fmt.Printf("Config already exists at %s\n", configPath)
			}

			if commandmentsWritten {
				fmt.Printf("Commandments written to %s\n", commandmentsPath)
			} else {
				fmt.Printf("Commandments already exist at %s\n", commandmentsPath)
			}

			if toolRegistryWritten {
				fmt.Printf("Tool registry written to %s\n", toolRegistryPath)
			} else {
				fmt.Printf("Tool registry already exists at %s\n", toolRegistryPath)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config")
	return cmd
}

func writeDefaultFile(path string, content []byte, force bool) (bool, error) {
	if _, err := os.Stat(path); err == nil && !force {
		return false, nil
	}

	if err := os.WriteFile(path, content, 0600); err != nil {
		return false, err
	}

	return true, nil
}

func defaultInitConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "crabwise")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "crabwise")
}

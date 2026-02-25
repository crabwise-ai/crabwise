package cli

import (
	"fmt"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/spf13/cobra"
)

func newEnvCmd() *cobra.Command {
	var (
		configPath string
		shell      string
	)

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Print proxy environment variables for shell evaluation",
		Example: `  eval $(crabwise env)
  eval $(crabwise env --shell bash)
  crabwise env --shell fish | source`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			for _, p := range proxyEnvPairs(cfg) {
				switch shell {
				case "fish":
					fmt.Printf("set -gx %s %q\n", p.key, p.value)
				default:
					fmt.Printf("export %s=%q\n", p.key, p.value)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().StringVar(&shell, "shell", "bash", "output format: bash, zsh, or fish")
	return cmd
}

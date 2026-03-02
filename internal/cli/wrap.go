package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/service"
	"github.com/spf13/cobra"
)

// envConfigFromDaemon constructs a service.EnvConfig from the daemon config.
// Used by wrap, env, and service inject commands.
func envConfigFromDaemon(cfg *daemon.Config) service.EnvConfig {
	return service.EnvConfig{
		ProxyURL: "http://" + cfg.Adapters.Proxy.Listen,
		CACert:   cfg.Adapters.Proxy.CACert,
	}
}

func overlayEnv(base []string, vars []service.EnvVar) []string {
	overrides := make(map[string]string, len(vars))
	for _, v := range vars {
		overrides[v.Key] = v.Value
	}

	var result []string
	seen := make(map[string]bool)
	for _, entry := range base {
		k, _, _ := strings.Cut(entry, "=")
		if v, ok := overrides[k]; ok {
			result = append(result, k+"="+v)
			seen[k] = true
		} else {
			result = append(result, entry)
		}
	}
	for _, v := range vars {
		if !seen[v.Key] {
			result = append(result, v.Key+"="+v.Value)
		}
	}
	return result
}

func newWrapCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "wrap -- <command> [args...]",
		Short: "Run a command with proxy environment configured",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("no command specified; usage: crabwise wrap -- <command> [args...]")
			}

			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			binary, err := exec.LookPath(args[0])
			if err != nil {
				return fmt.Errorf("resolve command %q: %w", args[0], err)
			}

			env := overlayEnv(os.Environ(), service.ProxyEnvVars(envConfigFromDaemon(cfg)))
			return syscall.Exec(binary, args, env)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

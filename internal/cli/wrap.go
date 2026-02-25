package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/spf13/cobra"
)

type envPair struct {
	key, value string
}

func proxyEnvPairs(cfg *daemon.Config) []envPair {
	proxyURL := "http://" + cfg.Adapters.Proxy.Listen
	pairs := []envPair{
		{"HTTPS_PROXY", proxyURL},
		{"HTTP_PROXY", proxyURL},
		{"ALL_PROXY", proxyURL},
		{"https_proxy", proxyURL},
		{"http_proxy", proxyURL},
		{"all_proxy", proxyURL},
	}
	if cfg.Adapters.Proxy.CACert != "" {
		pairs = append(pairs, envPair{"NODE_EXTRA_CA_CERTS", cfg.Adapters.Proxy.CACert})
	}
	pairs = append(pairs,
		envPair{"NO_PROXY", "localhost,127.0.0.1"},
		envPair{"no_proxy", "localhost,127.0.0.1"},
	)
	return pairs
}

func overlayEnv(base []string, pairs []envPair) []string {
	overrides := make(map[string]string, len(pairs))
	for _, p := range pairs {
		overrides[p.key] = p.value
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
	for _, p := range pairs {
		if !seen[p.key] {
			result = append(result, p.key+"="+p.value)
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

			env := overlayEnv(os.Environ(), proxyEnvPairs(cfg))
			return syscall.Exec(binary, args, env)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

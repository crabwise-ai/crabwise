package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crabwise-ai/crabwise/configs"
	"github.com/crabwise-ai/crabwise/internal/adapter/proxy"
	"github.com/crabwise-ai/crabwise/internal/daemon"
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
			mappingsDir := filepath.Join(configDir, "proxy_mappings")
			openaiMappingPath := filepath.Join(mappingsDir, "openai.yaml")

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
			if err := os.MkdirAll(mappingsDir, 0700); err != nil {
				return fmt.Errorf("create mappings dir: %w", err)
			}
			openaiMappingWritten, err := writeDefaultFile(openaiMappingPath, configs.DefaultOpenAIProxyMappingYAML, force)
			if err != nil {
				return fmt.Errorf("write openai mapping: %w", err)
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
			if openaiMappingWritten {
				fmt.Printf("OpenAI proxy mapping written to %s\n", openaiMappingPath)
			} else {
				fmt.Printf("OpenAI proxy mapping already exists at %s\n", openaiMappingPath)
			}

			// Resolve CA cert/key paths from config (already tilde-expanded).
			var certPath, keyPath string
			if cfg, err := daemon.LoadConfig(""); err == nil {
				certPath = cfg.Adapters.Proxy.CACert
				keyPath = cfg.Adapters.Proxy.CAKey
			} else {
				home, _ := os.UserHomeDir()
				certPath = filepath.Join(home, ".local", "share", "crabwise", "ca.crt")
				keyPath = filepath.Join(home, ".local", "share", "crabwise", "ca.key")
			}

			_, certErr := os.Stat(certPath)
			_, keyErr := os.Stat(keyPath)
			caExists := certErr == nil && keyErr == nil

			if caExists && !force {
				fmt.Printf("CA certificate already exists at %s\n", certPath)
			} else {
				if err := proxy.GenerateCA(certPath, keyPath); err != nil {
					return fmt.Errorf("generate CA: %w", err)
				}
				fmt.Printf("CA certificate generated at %s\n", certPath)
				fmt.Printf("CA key generated at %s\n", keyPath)
				fmt.Printf("\nTo trust the CA certificate:\n")
				fmt.Printf("  Linux:   sudo cp %s /usr/local/share/ca-certificates/crabwise.crt && sudo update-ca-certificates\n", certPath)
				fmt.Printf("  macOS:   sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain %s\n", certPath)
				fmt.Printf("  Node.js: export NODE_EXTRA_CA_CERTS=%s\n", certPath)
				fmt.Printf("Or use: crabwise wrap -- <command>  (sets proxy env vars automatically)\n")
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

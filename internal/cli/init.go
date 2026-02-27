package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crabwise-ai/crabwise/configs"
	"github.com/crabwise-ai/crabwise/internal/certs"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/tui"
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

			styled := !isPlain()

			if styled {
				fmt.Println(tui.RenderBannerStatic(Version))
				fmt.Println()
			}

			printInitFile(styled, configWritten, "Config", configPath)
			printInitFile(styled, commandmentsWritten, "Commandments", commandmentsPath)
			printInitFile(styled, toolRegistryWritten, "Tool registry", toolRegistryPath)
			printInitFile(styled, openaiMappingWritten, "OpenAI proxy mapping", openaiMappingPath)

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
				if styled {
					fmt.Printf("  %s %s\n", tui.StyleMuted.Render("○"), tui.StyleMuted.Render("CA certificate already exists at "+certPath))
				} else {
					fmt.Printf("CA certificate already exists at %s\n", certPath)
				}
			} else {
				if err := certs.GenerateCA(certPath, keyPath); err != nil {
					return fmt.Errorf("generate CA: %w", err)
				}
				if styled {
					fmt.Printf("  %s %s %s\n", tui.StyleSuccess.Render("✓"), tui.StyleBody.Render("CA certificate"), tui.StyleMuted.Render(certPath))
					fmt.Printf("  %s %s %s\n", tui.StyleSuccess.Render("✓"), tui.StyleBody.Render("CA key"), tui.StyleMuted.Render(keyPath))
				} else {
					fmt.Printf("CA certificate generated at %s\n", certPath)
					fmt.Printf("CA key generated at %s\n", keyPath)
				}
			}

			commands := certs.CommandsForOS(certPath)
			status := certs.CheckTrust(certPath, keyPath)
			if !status.Trusted && commands.SystemTrustCmd != "" {
				if styled {
					body := "Copy/paste:\n" + commands.SystemTrustCmd + "\n\n" +
						"Node.js (optional):\n" + commands.NodeExtraCACerts + "\n\n" +
						"Or copy it:\ncrabwise cert trust --copy\n" +
						"Or wrap your agent:\ncrabwise wrap -- <command>"
					fmt.Println()
					fmt.Println(tui.RenderPanel("Trust the CA ("+commands.OS+")", body))
				} else {
					fmt.Printf("\nTo trust the CA certificate (%s):\n", commands.OS)
					fmt.Printf("%s\n", commands.SystemTrustCmd)
					fmt.Printf("\nNode.js (optional):\n%s\n", commands.NodeExtraCACerts)
					fmt.Printf("\nOr copy it:\ncrabwise cert trust --copy\n")
					fmt.Printf("Or wrap your agent:\ncrabwise wrap -- <command>\n")
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config")
	return cmd
}

func printInitFile(styled, written bool, label, path string) {
	if styled {
		if written {
			fmt.Printf("  %s %s %s\n", tui.StyleSuccess.Render("✓"), tui.StyleBody.Render(label), tui.StyleMuted.Render(path))
		} else {
			fmt.Printf("  %s %s\n", tui.StyleMuted.Render("○"), tui.StyleMuted.Render(label+" already exists at "+path))
		}
	} else {
		if written {
			fmt.Printf("%s written to %s\n", label, path)
		} else {
			fmt.Printf("%s already exists at %s\n", label, path)
		}
	}
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

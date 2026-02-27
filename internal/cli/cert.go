package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/crabwise-ai/crabwise/internal/certs"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

func newCertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Manage the Crabwise CA certificate",
	}

	cmd.AddCommand(
		newCertStatusCmd(),
		newCertTrustCmd(),
	)

	return cmd
}

func newCertStatusCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show CA certificate status (exists + trusted)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			c := certs.CommandsForOS(cfg.Adapters.Proxy.CACert)
			status := certs.CheckTrust(cfg.Adapters.Proxy.CACert, cfg.Adapters.Proxy.CAKey)

			if isPlain() {
				fmt.Printf("OS: %s\n", c.OS)
				fmt.Printf("CA cert: %s\n", status.CertPath)
				fmt.Printf("CA key:  %s\n", status.KeyPath)
				fmt.Printf("Exists:  %t\n", status.Exists)
				fmt.Printf("Trusted: %t (%s)\n", status.Trusted, status.Reason)
				if c.SystemTrustCmd != "" {
					fmt.Printf("\nTrust command:\n%s\n", c.SystemTrustCmd)
					if certs.HasClipboard() {
						fmt.Printf("Copy to clipboard: crabwise cert trust --copy\n")
					}
				}
				return nil
			}

			fmt.Printf("  %s %s %s\n", tui.StatusIcon(boolToStatus(status.Exists)), tui.StyleBody.Render("CA certificate"), tui.StyleMuted.Render(status.CertPath))
			fmt.Printf("  %s %s %s\n", tui.StatusIcon(boolToStatus(status.Exists)), tui.StyleBody.Render("CA key"), tui.StyleMuted.Render(status.KeyPath))
			fmt.Printf("  %s %s %s\n", tui.StatusIcon(boolToStatus(status.Trusted)), tui.StyleBody.Render("Trusted by system"), tui.StyleMuted.Render(status.Reason))

			if c.SystemTrustCmd != "" && !status.Trusted {
				body := "Copy/paste:\n" + c.SystemTrustCmd
				if certs.HasClipboard() {
					body += "\n\nOr copy it:\ncrabwise cert trust --copy"
				}
				fmt.Println()
				fmt.Println(tui.RenderPanel("Trust the CA ("+c.OS+")", body))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

func newCertTrustCmd() *cobra.Command {
	var configPath string
	var copy bool
	var run bool
	var force bool

	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Print (and optionally copy) the OS trust command",
		Long:  "Ensures the local Crabwise CA exists, then prints a single command you can run to trust it on your OS.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			certPath := cfg.Adapters.Proxy.CACert
			keyPath := cfg.Adapters.Proxy.CAKey

			_, certErr := os.Stat(certPath)
			_, keyErr := os.Stat(keyPath)
			caExists := certErr == nil && keyErr == nil

			if !caExists || force {
				if err := certs.GenerateCA(certPath, keyPath); err != nil {
					return fmt.Errorf("generate CA: %w", err)
				}
				if isPlain() {
					fmt.Printf("CA certificate generated at %s\n", certPath)
					fmt.Printf("CA key generated at %s\n", keyPath)
				} else {
					fmt.Printf("  %s %s %s\n", tui.StatusIcon("success"), tui.StyleBody.Render("CA certificate"), tui.StyleMuted.Render(certPath))
					fmt.Printf("  %s %s %s\n", tui.StatusIcon("success"), tui.StyleBody.Render("CA key"), tui.StyleMuted.Render(keyPath))
				}
			}

			commands := certs.CommandsForOS(certPath)

			if commands.SystemTrustCmd == "" {
				if isPlain() {
					fmt.Printf("No automatic trust command detected for OS %q.\n", commands.OS)
					fmt.Printf("Manually add this CA certificate to your system trust store:\n%s\n", certPath)
				} else {
					body := "No automatic trust command detected for this OS.\n\n" +
						"Manually add this CA certificate to your system trust store:\n" + certPath +
						"\n\nNode.js (optional):\n" + commands.NodeExtraCACerts
					fmt.Println()
					fmt.Println(tui.RenderPanel("Trust the CA ("+commands.OS+")", body))
				}
				return nil
			}

			if isPlain() {
				fmt.Println(commands.SystemTrustCmd)
			} else {
				body := "Copy/paste:\n" + commands.SystemTrustCmd + "\n\nNode.js (optional):\n" + commands.NodeExtraCACerts
				fmt.Println()
				fmt.Println(tui.RenderPanel("Trust the CA ("+commands.OS+")", body))
			}

			if copy {
				method, err := certs.CopyToClipboard(commands.SystemTrustCmd)
				if err != nil {
					return fmt.Errorf("copy to clipboard: %w", err)
				}
				if isPlain() {
					fmt.Printf("Copied trust command to clipboard via %s\n", method)
				} else {
					fmt.Printf("\n  %s %s %s\n", tui.StatusIcon("success"), tui.StyleBody.Render("Copied to clipboard"), tui.StyleMuted.Render(method))
				}
			}

			if run {
				if isPlain() {
					fmt.Printf("Running trust command (may prompt for admin password)...\n")
				} else {
					fmt.Printf("\n  %s %s\n", tui.StatusIcon("connecting"), tui.StyleBody.Render("Running trust command (may prompt for admin password)..."))
				}

				var execCmd *exec.Cmd
				if runtime.GOOS == "windows" {
					execCmd = exec.Command("cmd.exe", "/C", commands.SystemTrustCmd)
				} else {
					execCmd = exec.Command("sh", "-c", commands.SystemTrustCmd)
				}
				execCmd.Stdout = os.Stdout
				execCmd.Stderr = os.Stderr
				execCmd.Stdin = os.Stdin
				if err := execCmd.Run(); err != nil {
					return fmt.Errorf("trust command failed: %w", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().BoolVar(&copy, "copy", false, "copy trust command to clipboard")
	cmd.Flags().BoolVar(&run, "run", false, "run trust command automatically (may require admin)")
	cmd.Flags().BoolVar(&force, "force", false, "regenerate CA before trusting")
	return cmd
}

func boolToStatus(b bool) string {
	if b {
		return "success"
	}
	return "warning"
}

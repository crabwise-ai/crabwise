package cli

import (
	"fmt"
	"os"
	"runtime"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/service"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

// Test hooks for dependency injection. Production defaults use real implementations.
var (
	detectManagerFn = service.DetectManager
	getUIDFn        = os.Getuid
	getSUDOUserFn   = func() string { return os.Getenv("SUDO_USER") }
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage proxy injection for system and user services",
	}

	cmd.AddCommand(
		newServiceInjectCmd(),
		newServiceRemoveCmd(),
		newServiceStatusCmd(),
	)

	return cmd
}

func newServiceInjectCmd() *cobra.Command {
	var scopeFlag, agentName, configPath string
	var restart bool

	cmd := &cobra.Command{
		Use:   "inject",
		Short: "Inject proxy environment into a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scope, err := service.ParseScope(scopeFlag)
			if err != nil {
				return err
			}

			if err := service.ValidatePrivileges(scope, getUIDFn(), getSUDOUserFn()); err != nil {
				if scope == service.ScopeSystem {
					fmt.Fprintf(os.Stderr, "hint: %s\n",
						service.SuggestElevatedCommand(os.Args))
				}
				return err
			}

			mgr := detectManagerFn()
			if mgr == nil {
				return fmt.Errorf("unsupported operating system")
			}

			serviceName := service.ResolveAgentName(agentName, cfg.Service.Agents, runtime.GOOS)
			res, err := mgr.Resolve(serviceName, scope)
			if err != nil {
				return err
			}

			envCfg := envConfigFromDaemon(cfg)
			result, err := mgr.Inject(res, envCfg)
			if err != nil {
				return err
			}

			if isPlain() {
				fmt.Printf("injected: %s\n", result.Path)
			} else {
				fmt.Printf("  %s %s %s\n",
					tui.StatusIcon("success"),
					tui.StyleBody.Render("Proxy injected"),
					tui.StyleMuted.Render(result.Path))
			}

			if restart {
				if err := mgr.Restart(res); err != nil {
					return fmt.Errorf("inject succeeded but restart failed: %w", err)
				}
				if isPlain() {
					fmt.Printf("restarted: %s\n", serviceName)
				} else {
					fmt.Printf("  %s %s\n",
						tui.StatusIcon("success"),
						tui.StyleBody.Render("Service restarted"))
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&scopeFlag, "scope", "system", "service scope: system or user")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent name or literal service name (required)")
	_ = cmd.MarkFlagRequired("agent")
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart service after inject")
	return cmd
}

func newServiceRemoveCmd() *cobra.Command {
	var scopeFlag, agentName, configPath string
	var restart bool

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove proxy injection from a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scope, err := service.ParseScope(scopeFlag)
			if err != nil {
				return err
			}

			if err := service.ValidatePrivileges(scope, getUIDFn(), getSUDOUserFn()); err != nil {
				if scope == service.ScopeSystem {
					fmt.Fprintf(os.Stderr, "hint: %s\n",
						service.SuggestElevatedCommand(os.Args))
				}
				return err
			}

			mgr := detectManagerFn()
			if mgr == nil {
				return fmt.Errorf("unsupported operating system")
			}

			serviceName := service.ResolveAgentName(agentName, cfg.Service.Agents, runtime.GOOS)
			res, err := mgr.Resolve(serviceName, scope)
			if err != nil {
				return err
			}

			envCfg := envConfigFromDaemon(cfg)
			result, err := mgr.Remove(res, envCfg)
			if err != nil {
				return err
			}

			if isPlain() {
				if result.Removed {
					fmt.Printf("removed: %s\n", result.Path)
				} else {
					fmt.Printf("not injected: %s\n", result.Path)
				}
			} else {
				if result.Removed {
					fmt.Printf("  %s %s %s\n",
						tui.StatusIcon("success"),
						tui.StyleBody.Render("Proxy removed"),
						tui.StyleMuted.Render(result.Path))
				} else {
					fmt.Printf("  %s %s\n",
						tui.StatusIcon("warning"),
						tui.StyleBody.Render("Not injected — nothing to remove"))
				}
			}

			if restart && result.Removed {
				if err := mgr.Restart(res); err != nil {
					return fmt.Errorf("remove succeeded but restart failed: %w", err)
				}
				if isPlain() {
					fmt.Printf("restarted: %s\n", serviceName)
				} else {
					fmt.Printf("  %s %s\n",
						tui.StatusIcon("success"),
						tui.StyleBody.Render("Service restarted"))
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&scopeFlag, "scope", "system", "service scope: system or user")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent name or literal service name (required)")
	_ = cmd.MarkFlagRequired("agent")
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart service after remove")
	return cmd
}

func newServiceStatusCmd() *cobra.Command {
	var scopeFlag, agentName, configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show proxy injection status for a service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scope, err := service.ParseScope(scopeFlag)
			if err != nil {
				return err
			}

			mgr := detectManagerFn()
			if mgr == nil {
				return fmt.Errorf("unsupported operating system")
			}

			serviceName := service.ResolveAgentName(agentName, cfg.Service.Agents, runtime.GOOS)
			res, err := mgr.Resolve(serviceName, scope)
			if err != nil {
				if isPlain() {
					fmt.Printf("agent: %s\nscope: %s\nservice: %s\nresolved: false\n",
						agentName, scope, serviceName)
				} else {
					fmt.Printf("  %s %s %s\n",
						tui.StatusIcon("warning"),
						tui.StyleBody.Render("Not found"),
						tui.StyleMuted.Render(serviceName+" in "+string(scope)+" scope"))
				}
				return nil
			}

			injected, checkErr := mgr.CheckInjected(res)

			if isPlain() {
				if checkErr != nil {
					fmt.Printf("agent: %s\nscope: %s\nservice: %s\nresolved: true\ninjected: unknown\nerror: %s\n",
						agentName, scope, serviceName, checkErr)
				} else {
					fmt.Printf("agent: %s\nscope: %s\nservice: %s\nresolved: true\ninjected: %t\n",
						agentName, scope, serviceName, injected)
				}
			} else {
				fmt.Printf("  %s %s %s\n",
					tui.StatusIcon("success"),
					tui.StyleBody.Render("Resolved"),
					tui.StyleMuted.Render(agentName+" → "+serviceName+" in "+string(scope)+" scope"))
				if checkErr != nil {
					fmt.Printf("  %s %s %s\n",
						tui.StatusIcon("warning"),
						tui.StyleBody.Render("Status unknown"),
						tui.StyleMuted.Render(checkErr.Error()))
				} else {
					fmt.Printf("  %s %s\n",
						tui.StatusIcon(boolToStatus(injected)),
						tui.StyleBody.Render("Proxy "+map[bool]string{true: "injected", false: "not injected"}[injected]))
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&scopeFlag, "scope", "system", "service scope: system or user")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent name or literal service name (required)")
	_ = cmd.MarkFlagRequired("agent")
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

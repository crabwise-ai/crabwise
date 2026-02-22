package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			data, err := os.ReadFile(cfg.Daemon.PIDFile)
			if err != nil {
				return fmt.Errorf("no running daemon (read pid: %w)", err)
			}

			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				return fmt.Errorf("invalid pid file: %w", err)
			}

			process, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("find process %d: %w", pid, err)
			}

			if err := process.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("send SIGTERM to %d: %w", pid, err)
			}

			fmt.Printf("Sent SIGTERM to pid %d, waiting...\n", pid)

			// Wait for process to exit
			for i := 0; i < 30; i++ {
				if err := process.Signal(syscall.Signal(0)); err != nil {
					fmt.Println("Daemon stopped.")
					return nil
				}
				time.Sleep(100 * time.Millisecond)
			}

			return fmt.Errorf("daemon pid %d did not stop within 3s", pid)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	return cmd
}

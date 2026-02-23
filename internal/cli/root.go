package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

const orange = "\033[38;5;208m"
const reset = "\033[0m"

func banner() string {
	return "" +
		orange + "▄█▀      ▀█▄" + reset + "  Crabwise AI v" + Version + "\n" +
		orange + "█▄█ ▄  ▄ █▄█" + reset + "  Monitor and audit AI agent activity\n" +
		orange + "█▀ ▄█▄▄█▄ ▀█" + reset + "  https://github.com/crabwise-ai/crabwise\n" +
		orange + "▀██████████▀" + reset + "\n" +
		orange + " ▄████████▄" + reset + "\n" +
		orange + "█  █    █  █" + reset + "\n"
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "crabwise",
		Short: "Monitor and audit AI agent activity",
		Long:  banner(),
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	root.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newAgentsCmd(),
		newAuditCmd(),
		newWatchCmd(),
	)

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Version)
		},
	}
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "crabwise",
		Short: "Monitor and audit AI agent activity",
		Long:  "Crabwise is a local-first daemon that monitors AI agent activity, maintains a tamper-evident audit trail, and enforces safety rules.",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	root.AddCommand(
		newVersionCmd(),
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

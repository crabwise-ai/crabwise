package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

// TODO: Replace with ASCII art logo
const logo = `
               ffff            ffff
             ffff                ffff
             ff  ff            ff  ff
             GGGGGG  GG    GG  GGGGGG
             GGGG    GG    GG    GGGG
             GG    GGGGGGGGGGGG    GG
             tttttttttttttttttttttttt
               tttttttttttttttttttt
                 tttttttttttttttt
               11111111111111111111
             11    11        11    11
             11    11        11    11
`

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "crabwise",
		Short: "Monitor and audit AI agent activity",
		Long:  logo + "Crabwise is a local-first daemon that monitors AI agent activity, maintains a tamper-evident audit trail, and enforces safety rules.",
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

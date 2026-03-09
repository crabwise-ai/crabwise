package cli

import (
	"fmt"
	"os"

	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

// PlainMode disables styled output when set via --plain flag.
var PlainMode bool

// isPlain returns true if output should be plain text (no ANSI escape codes).
func isPlain() bool {
	if PlainMode {
		return true
	}
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "crabwise",
		Short: "Monitor and audit AI agent activity",
		Long:  tui.RenderBannerStatic(Version),
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}

	root.PersistentFlags().BoolVar(&PlainMode, "plain", false, "disable styled output")

	root.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newCertCmd(),
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newClassifyCmd(),
		newAgentsCmd(),
		newCommandmentsCmd(),
		newAuditCmd(),
		newWatchCmd(),
		newWrapCmd(),
		newEnvCmd(),
		newServiceCmd(),
		newSettingsCmd(),
	)

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			if isPlain() {
				fmt.Println(Version)
				return
			}
			fmt.Println(tui.StyleWarning.Render("🦀") + " " + tui.StyleBody.Render("Crabwise AI v"+Version))
		},
	}
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

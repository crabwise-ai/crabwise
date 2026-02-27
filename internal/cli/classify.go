package cli

import (
	"fmt"
	"strings"

	"github.com/crabwise-ai/crabwise/configs"
	"github.com/crabwise-ai/crabwise/internal/classify"
	"github.com/crabwise-ai/crabwise/internal/daemon"
	"github.com/crabwise-ai/crabwise/internal/tui"
	"github.com/spf13/cobra"
)

func newClassifyCmd() *cobra.Command {
	var (
		configPath string
		provider   string
		argKeysCSV string
	)

	cmd := &cobra.Command{
		Use:   "classify <tool-name>",
		Short: "Classify a tool name against the active taxonomy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := daemon.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			registry, err := classify.LoadRegistry(cfg.ToolRegistry.File, configs.DefaultToolRegistryYAML)
			if err != nil {
				return fmt.Errorf("load tool registry: %w", err)
			}

			argKeys := classify.NormalizeArgKeys(parseCSV(argKeysCSV))
			result := registry.Classify(provider, args[0], argKeys)
			providerDisplay := strings.TrimSpace(provider)
			if providerDisplay == "" {
				providerDisplay = "_default"
			}

			if isPlain() {
				fmt.Printf("Tool:      %s\n", args[0])
				fmt.Printf("Provider:  %s\n", providerDisplay)
				fmt.Printf("Arg keys:  %s\n", strings.Join(argKeys, ","))
				fmt.Printf("Category:  %s\n", result.Category)
				fmt.Printf("Effect:    %s\n", result.Effect)
				fmt.Printf("Source:    %s\n", result.ClassificationSource)
				fmt.Printf("Taxonomy:  %s\n", result.TaxonomyVersion)
			} else {
				body := fmt.Sprintf(
					"%s  %s\n%s  %s\n%s  %s\n%s  %s\n%s  %s\n%s  %s\n%s  %s",
					tui.StyleHeading.Render("Tool:"), tui.StyleBody.Render(args[0]),
					tui.StyleHeading.Render("Provider:"), tui.StyleBody.Render(providerDisplay),
					tui.StyleHeading.Render("Arg keys:"), tui.StyleBody.Render(strings.Join(argKeys, ",")),
					tui.StyleHeading.Render("Category:"), tui.StyleBody.Render(result.Category),
					tui.StyleHeading.Render("Effect:"), tui.StyleBody.Render(result.Effect),
					tui.StyleHeading.Render("Source:"), tui.StyleBody.Render(result.ClassificationSource),
					tui.StyleHeading.Render("Taxonomy:"), tui.StyleBody.Render(result.TaxonomyVersion),
				)
				fmt.Println(tui.RenderPanel("Classification", body))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "config file path")
	cmd.Flags().StringVar(&provider, "provider", "", "tool provider (for example: anthropic, openai)")
	cmd.Flags().StringVar(&argKeysCSV, "args", "", "comma-separated argument keys")

	return cmd
}

func parseCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

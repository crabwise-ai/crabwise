package cli

import (
	"fmt"
	"strings"

	"github.com/crabwise-ai/crabwise/configs"
	"github.com/crabwise-ai/crabwise/internal/classify"
	"github.com/crabwise-ai/crabwise/internal/daemon"
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

			fmt.Printf("Tool:      %s\n", args[0])
			fmt.Printf("Provider:  %s\n", provider)
			fmt.Printf("Arg keys:  %s\n", strings.Join(argKeys, ","))
			fmt.Printf("Category:  %s\n", result.Category)
			fmt.Printf("Effect:    %s\n", result.Effect)
			fmt.Printf("Source:    %s\n", result.ClassificationSource)
			fmt.Printf("Taxonomy:  %s\n", result.TaxonomyVersion)

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

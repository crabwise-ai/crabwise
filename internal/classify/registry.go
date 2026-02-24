package classify

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const defaultProviderKey = "_default"

type Registry struct {
	version      string
	providers    map[string]compiledProvider
	heuristics   []compiledRule
	unclassified atomic.Uint64
}

type RegistryConfig struct {
	Version    string                      `yaml:"version"`
	Providers  map[string]ProviderRegistry `yaml:"providers"`
	Heuristics []RuleSpec                  `yaml:"heuristics"`
}

type ProviderRegistry struct {
	Tools    map[string]ToolMapping `yaml:"tools"`
	Patterns []RuleSpec             `yaml:"patterns,omitempty"`
}

type compiledProvider struct {
	Tools    map[string]ToolMapping
	Patterns []compiledRule
}

type ToolMapping struct {
	Category string `yaml:"category"`
	Effect   string `yaml:"effect"`
}

type RuleSpec struct {
	Match RuleMatch   `yaml:"match"`
	Set   ToolMapping `yaml:"set"`
}

type RuleMatch struct {
	NameGlob   []string `yaml:"name_glob,omitempty"`
	ArgKeysAny []string `yaml:"arg_keys_any,omitempty"`
}

type compiledRule struct {
	nameGlobs   []string
	argKeysAny  map[string]struct{}
	mapping     ToolMapping
	source      string
	description string
}

func LoadRegistry(path string, fallbackYAML []byte) (*Registry, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			return LoadRegistryYAML(data)
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if len(fallbackYAML) == 0 {
		return nil, fmt.Errorf("no tool registry source available")
	}

	return LoadRegistryYAML(fallbackYAML)
}

func LoadRegistryYAML(data []byte) (*Registry, error) {
	var cfg RegistryConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return NewRegistry(cfg)
}

func NewRegistry(cfg RegistryConfig) (*Registry, error) {
	r := &Registry{
		version:   normalizeTaxonomyVersion(cfg.Version),
		providers: make(map[string]compiledProvider, len(cfg.Providers)),
	}

	if r.version == "" {
		r.version = DefaultTaxonomyVersion
	}

	for provider, providerRegistry := range cfg.Providers {
		providerKey := normalizeProvider(provider)
		if providerKey == "" {
			continue
		}

		compiledProvider, err := compileProvider(providerRegistry, providerKey)
		if err != nil {
			return nil, err
		}
		r.providers[providerKey] = compiledProvider
	}

	heuristics, err := compileRules(cfg.Heuristics, SourceHeuristic, "heuristics")
	if err != nil {
		return nil, err
	}
	r.heuristics = heuristics

	warnHeuristicOverlaps(heuristics)

	return r, nil
}

func NewFallbackRegistry() *Registry {
	return &Registry{
		version:   DefaultTaxonomyVersion,
		providers: map[string]compiledProvider{},
	}
}

func (r *Registry) Version() string {
	if r == nil || r.version == "" {
		return DefaultTaxonomyVersion
	}
	return r.version
}

func (r *Registry) UnclassifiedCount() uint64 {
	if r == nil {
		return 0
	}
	return r.unclassified.Load()
}

func (r *Registry) Classify(provider, toolName string, argKeys []string) ClassifyResult {
	if r == nil {
		return fallbackResult(DefaultTaxonomyVersion)
	}

	providerKey := normalizeProvider(provider)
	lowerTool := strings.ToLower(strings.TrimSpace(toolName))
	argKeysSet := toSet(NormalizeArgKeys(argKeys))

	if providerRegistry, ok := r.providers[providerKey]; ok {
		if mapping, ok := providerRegistry.Tools[toolName]; ok {
			return r.resultFor(mapping, SourceExact)
		}
		if mapping, ok := providerRegistry.Tools[lowerTool]; ok {
			return r.resultFor(mapping, SourceExact)
		}
	}

	if defaultProvider, ok := r.providers[defaultProviderKey]; ok {
		if mapping, ok := defaultProvider.Tools[lowerTool]; ok {
			return r.resultFor(mapping, SourceExact)
		}
	}

	if result, ok := r.matchPattern(providerKey, lowerTool, argKeysSet); ok {
		return result
	}

	if mapping, ok := matchRules(r.heuristics, lowerTool, argKeysSet); ok {
		return r.resultFor(mapping, SourceHeuristic)
	}

	r.unclassified.Add(1)
	return fallbackResult(r.version)
}

func (r *Registry) resultFor(mapping ToolMapping, source string) ClassifyResult {
	return ClassifyResult{
		Category:             mapping.Category,
		Effect:               mapping.Effect,
		TaxonomyVersion:      r.version,
		ClassificationSource: source,
	}
}

func (r *Registry) matchPattern(provider, lowerTool string, argKeysSet map[string]struct{}) (ClassifyResult, bool) {
	if providerRegistry, ok := r.providers[provider]; ok {
		if mapping, ok := matchRules(providerRegistry.Patterns, lowerTool, argKeysSet); ok {
			return r.resultFor(mapping, SourcePattern), true
		}
	}

	if defaultProvider, ok := r.providers[defaultProviderKey]; ok {
		if mapping, ok := matchRules(defaultProvider.Patterns, lowerTool, argKeysSet); ok {
			return r.resultFor(mapping, SourcePattern), true
		}
	}

	return ClassifyResult{}, false
}

func compileProvider(provider ProviderRegistry, providerName string) (compiledProvider, error) {
	tools := make(map[string]ToolMapping, len(provider.Tools))
	for toolName, mapping := range provider.Tools {
		if strings.TrimSpace(toolName) == "" {
			continue
		}
		if err := validateMapping(mapping); err != nil {
			return compiledProvider{}, fmt.Errorf("provider %q tool %q: %w", providerName, toolName, err)
		}
		tools[toolName] = mapping
	}

	patterns, err := compileRules(provider.Patterns, SourcePattern, "provider."+providerName+".patterns")
	if err != nil {
		return compiledProvider{}, err
	}

	return compiledProvider{Tools: tools, Patterns: patterns}, nil
}

func compileRules(specs []RuleSpec, source, section string) ([]compiledRule, error) {
	compiled := make([]compiledRule, 0, len(specs))
	for idx, spec := range specs {
		if err := validateMapping(spec.Set); err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", section, idx, err)
		}

		rule := compiledRule{
			nameGlobs:   normalizeGlobs(spec.Match.NameGlob),
			argKeysAny:  toSet(NormalizeArgKeys(spec.Match.ArgKeysAny)),
			mapping:     spec.Set,
			source:      source,
			description: fmt.Sprintf("%s[%d]", section, idx),
		}

		if len(rule.nameGlobs) == 0 && len(rule.argKeysAny) == 0 {
			return nil, fmt.Errorf("%s[%d]: empty matcher", section, idx)
		}

		compiled = append(compiled, rule)
	}

	return compiled, nil
}

func validateMapping(mapping ToolMapping) error {
	if strings.TrimSpace(mapping.Category) == "" {
		return fmt.Errorf("category is required")
	}
	if strings.TrimSpace(mapping.Effect) == "" {
		return fmt.Errorf("effect is required")
	}
	return nil
}

func normalizeTaxonomyVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return defaultProviderKey
	}
	return provider
}

func normalizeGlobs(globs []string) []string {
	norm := make([]string, 0, len(globs))
	for _, pattern := range globs {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		norm = append(norm, pattern)
	}
	return norm
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}
	return set
}

func matchRules(rules []compiledRule, lowerTool string, argKeysSet map[string]struct{}) (ToolMapping, bool) {
	for _, rule := range rules {
		if !matchesRule(rule, lowerTool, argKeysSet) {
			continue
		}
		return rule.mapping, true
	}
	return ToolMapping{}, false
}

func matchesRule(rule compiledRule, lowerTool string, argKeysSet map[string]struct{}) bool {
	nameMatch := len(rule.nameGlobs) == 0
	if !nameMatch {
		for _, pattern := range rule.nameGlobs {
			matched, err := doublestar.Match(pattern, lowerTool)
			if err != nil {
				continue
			}
			if matched {
				nameMatch = true
				break
			}
		}
	}

	argMatch := len(rule.argKeysAny) == 0
	if !argMatch {
		for key := range rule.argKeysAny {
			if _, ok := argKeysSet[key]; ok {
				argMatch = true
				break
			}
		}
	}

	return nameMatch && argMatch
}

func warnHeuristicOverlaps(heuristics []compiledRule) {
	if len(heuristics) < 2 {
		return
	}

	for i := 0; i < len(heuristics)-1; i++ {
		for j := i + 1; j < len(heuristics); j++ {
			overlaps := sharedGlobs(heuristics[i].nameGlobs, heuristics[j].nameGlobs)
			if len(overlaps) == 0 {
				continue
			}
			sort.Strings(overlaps)
			log.Printf(
				"classify: heuristic overlap warning: rules %q and %q share name_glob patterns %v",
				heuristics[i].description,
				heuristics[j].description,
				overlaps,
			)
		}
	}
}

func sharedGlobs(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(left))
	for _, pattern := range left {
		seen[pattern] = struct{}{}
	}

	var overlaps []string
	for _, pattern := range right {
		if _, ok := seen[pattern]; ok {
			overlaps = append(overlaps, pattern)
		}
	}

	return overlaps
}

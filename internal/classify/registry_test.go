package classify

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"
)

func TestClassifyExactLookupOrder(t *testing.T) {
	r, err := NewRegistry(RegistryConfig{
		Version: "1",
		Providers: map[string]ProviderRegistry{
			"openai": {
				Tools: map[string]ToolMapping{
					"Bash": {Category: CategoryFileRead, Effect: EffectReadOnly},
					"bash": {Category: CategoryFileWrite, Effect: EffectMutation},
				},
			},
			"_default": {
				Tools: map[string]ToolMapping{
					"bash": {Category: CategoryShell, Effect: EffectExecute},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	result := r.Classify("openai", "Bash", nil)
	if result.ClassificationSource != SourceExact || result.Category != CategoryFileRead {
		t.Fatalf("expected provider case-sensitive exact match first, got %+v", result)
	}

	result = r.Classify("openai", "BASH", nil)
	if result.ClassificationSource != SourceExact || result.Category != CategoryFileWrite {
		t.Fatalf("expected provider lowercase exact match second, got %+v", result)
	}

	result = r.Classify("unknown", "BASH", nil)
	if result.ClassificationSource != SourceExact || result.Category != CategoryShell {
		t.Fatalf("expected default provider exact match third, got %+v", result)
	}
}

func TestClassifyPatternHeuristicAndFallback(t *testing.T) {
	r, err := NewRegistry(RegistryConfig{
		Version: "1",
		Providers: map[string]ProviderRegistry{
			"_default": {
				Patterns: []RuleSpec{
					{
						Match: RuleMatch{NameGlob: []string{"scan_*"}},
						Set:   ToolMapping{Category: CategoryFileSearch, Effect: EffectMetadata},
					},
				},
			},
		},
		Heuristics: []RuleSpec{
			{
				Match: RuleMatch{ArgKeysAny: []string{"command"}},
				Set:   ToolMapping{Category: CategoryShell, Effect: EffectExecute},
			},
		},
	})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	result := r.Classify("openai", "scan_repo", nil)
	if result.ClassificationSource != SourcePattern || result.Category != CategoryFileSearch {
		t.Fatalf("expected pattern match, got %+v", result)
	}

	result = r.Classify("openai", "custom_tool", []string{"command"})
	if result.ClassificationSource != SourceHeuristic || result.Category != CategoryShell {
		t.Fatalf("expected heuristic match, got %+v", result)
	}

	result = r.Classify("openai", "totally_unknown", nil)
	if result.ClassificationSource != SourceFallback || result.Category != CategoryOther || result.Effect != EffectUnknown {
		t.Fatalf("expected fallback classification, got %+v", result)
	}
	if got := r.UnclassifiedCount(); got != 1 {
		t.Fatalf("expected unclassified count 1, got %d", got)
	}
}

func TestClassifyHeuristicFirstMatchWins(t *testing.T) {
	r, err := NewRegistry(RegistryConfig{
		Version: "1",
		Heuristics: []RuleSpec{
			{
				Match: RuleMatch{NameGlob: []string{"*read*"}},
				Set:   ToolMapping{Category: CategoryFileRead, Effect: EffectReadOnly},
			},
			{
				Match: RuleMatch{NameGlob: []string{"*read*"}, ArgKeysAny: []string{"content"}},
				Set:   ToolMapping{Category: CategoryFileWrite, Effect: EffectMutation},
			},
		},
	})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	result := r.Classify("anthropic", "ReadAnything", []string{"content"})
	if result.ClassificationSource != SourceHeuristic {
		t.Fatalf("expected heuristic source, got %+v", result)
	}
	if result.Category != CategoryFileRead || result.Effect != EffectReadOnly {
		t.Fatalf("expected first heuristic rule to win, got %+v", result)
	}
}

func TestHeuristicOverlapWarningIsNonBlocking(t *testing.T) {
	var logs bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})

	r, err := NewRegistry(RegistryConfig{
		Version: "1",
		Heuristics: []RuleSpec{
			{
				Match: RuleMatch{NameGlob: []string{"*exec*"}},
				Set:   ToolMapping{Category: CategoryShell, Effect: EffectExecute},
			},
			{
				Match: RuleMatch{NameGlob: []string{"*exec*"}},
				Set:   ToolMapping{Category: CategoryCodeExec, Effect: EffectExecute},
			},
		},
	})
	if err != nil {
		t.Fatalf("new registry should still succeed: %v", err)
	}
	if r == nil {
		t.Fatal("expected registry")
	}
	if !strings.Contains(logs.String(), "heuristic overlap warning") {
		t.Fatalf("expected overlap warning log, got: %q", logs.String())
	}
}

func TestExtractArgKeysDeterministic(t *testing.T) {
	raw := json.RawMessage(`{"Command":"ls","nested":{"File_Path":"a.txt"},"items":[{"URL":"https://example.com"},{"cmd":"pwd"}],"simple":"x"}`)
	keys := ExtractArgKeys(raw)
	want := []string{"cmd", "command", "file_path", "items", "nested", "simple", "url"}
	if len(keys) != len(want) {
		t.Fatalf("unexpected key count: got %v want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("unexpected key order/content: got %v want %v", keys, want)
		}
	}
}

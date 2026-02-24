package classify

const (
	CategoryShell      = "shell"
	CategoryFileRead   = "file.read"
	CategoryFileWrite  = "file.write"
	CategoryFileEdit   = "file.edit"
	CategoryFileSearch = "file.search"
	CategoryNetwork    = "network"
	CategoryCodeExec   = "code.exec"
	CategoryBrowser    = "browser"
	CategoryOther      = "other"
)

const (
	EffectReadOnly = "read_only"
	EffectMetadata = "metadata"
	EffectMutation = "mutation"
	EffectExecute  = "execute"
	EffectUnknown  = "unknown"
)

const (
	SourceExact     = "exact"
	SourcePattern   = "pattern"
	SourceHeuristic = "heuristic"
	SourceFallback  = "fallback"
)

const DefaultTaxonomyVersion = "v1"

type ClassifyResult struct {
	Category             string
	Effect               string
	TaxonomyVersion      string
	ClassificationSource string
}

type Classifier interface {
	Classify(provider, toolName string, argKeys []string) ClassifyResult
	UnclassifiedCount() uint64
}

func fallbackResult(version string) ClassifyResult {
	if version == "" {
		version = DefaultTaxonomyVersion
	}
	return ClassifyResult{
		Category:             CategoryOther,
		Effect:               EffectUnknown,
		TaxonomyVersion:      version,
		ClassificationSource: SourceFallback,
	}
}

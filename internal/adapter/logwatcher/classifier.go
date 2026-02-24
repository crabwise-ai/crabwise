package logwatcher

import (
	"encoding/json"
	"log"
	"sync/atomic"

	"github.com/crabwise-ai/crabwise/configs"
	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/classify"
)

var classifierValue atomic.Value

func init() {
	registry, err := classify.LoadRegistry("", configs.DefaultToolRegistryYAML)
	if err != nil {
		log.Printf("logwatcher: classifier init fallback: %v", err)
		registry = classify.NewFallbackRegistry()
	}
	classifierValue.Store(classify.Classifier(registry))
}

func SetClassifier(c classify.Classifier) {
	if c == nil {
		c = classify.NewFallbackRegistry()
	}
	classifierValue.Store(c)
}

func currentClassifier() classify.Classifier {
	v := classifierValue.Load()
	if c, ok := v.(classify.Classifier); ok && c != nil {
		return c
	}
	return classify.NewFallbackRegistry()
}

func applyToolClassification(e *audit.AuditEvent, provider, toolName string, rawArgs json.RawMessage) {
	if e == nil {
		return
	}

	e.Provider = provider
	e.ToolName = toolName

	result := currentClassifier().Classify(provider, toolName, classify.ExtractArgKeys(rawArgs))
	e.ToolCategory = result.Category
	e.ToolEffect = result.Effect
	e.TaxonomyVersion = result.TaxonomyVersion
	e.ClassificationSource = result.ClassificationSource
	e.ActionType = actionTypeForToolCategory(result.Category)
}

func actionTypeForToolCategory(category string) audit.ActionType {
	switch category {
	case classify.CategoryShell, classify.CategoryCodeExec:
		return audit.ActionCommandExecution
	case classify.CategoryFileRead, classify.CategoryFileWrite, classify.CategoryFileEdit, classify.CategoryFileSearch:
		return audit.ActionFileAccess
	default:
		return audit.ActionToolCall
	}
}

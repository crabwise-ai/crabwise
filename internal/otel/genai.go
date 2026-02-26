// Package crabwiseotel provides OpenTelemetry integration for Crabwise.
package crabwiseotel

// GenAI semantic convention attribute keys.
// Based on: https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/
// Status: Development (not yet stable in OTel semconv)
const (
	AttrGenAISystem             = "gen_ai.system"
	AttrGenAIOperationName      = "gen_ai.operation.name"
	AttrGenAIRequestModel       = "gen_ai.request.model"
	AttrGenAIResponseModel      = "gen_ai.response.model"
	AttrGenAIResponseID         = "gen_ai.response.id"
	AttrGenAIResponseFinish     = "gen_ai.response.finish_reasons"
	AttrGenAIUsageInputTokens   = "gen_ai.usage.input_tokens"
	AttrGenAIUsageOutputTokens  = "gen_ai.usage.output_tokens"
	AttrGenAIRequestMaxTokens   = "gen_ai.request.max_tokens"
	AttrGenAIRequestTemperature = "gen_ai.request.temperature"

	// Crabwise-specific extensions
	AttrCrabwiseOutcome   = "crabwise.outcome"
	AttrCrabwiseCostUSD   = "crabwise.cost_usd"
	AttrCrabwiseProvider  = "crabwise.provider"
	AttrCrabwiseAdapterID = "crabwise.adapter_id"
)

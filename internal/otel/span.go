package crabwiseotel

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// GenAISpanData holds the data needed to emit a GenAI span.
type GenAISpanData struct {
	System        string // e.g. "openai"
	Operation     string // e.g. "chat"
	RequestModel  string
	ResponseModel string
	ResponseID    string
	FinishReason  string
	InputTokens   int64
	OutputTokens  int64
	Outcome       string // success|failure|blocked|warned
	Provider      string
	AdapterID     string
}

// EmitGenAISpan creates a completed GenAI span. Call this after the proxy
// request finishes. The span is created and immediately ended with the
// recorded attributes.
func EmitGenAISpan(ctx context.Context, data GenAISpanData) {
	tracer := otel.Tracer("crabwise.proxy")
	spanName := data.Operation + " " + data.RequestModel

	_, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	attrs := []attribute.KeyValue{
		attribute.String(AttrGenAISystem, data.System),
		attribute.String(AttrGenAIOperationName, data.Operation),
		attribute.String(AttrGenAIRequestModel, data.RequestModel),
		attribute.String(AttrCrabwiseOutcome, data.Outcome),
		attribute.String(AttrCrabwiseProvider, data.Provider),
		attribute.String(AttrCrabwiseAdapterID, data.AdapterID),
	}

	if data.ResponseModel != "" {
		attrs = append(attrs, attribute.String(AttrGenAIResponseModel, data.ResponseModel))
	}
	if data.ResponseID != "" {
		attrs = append(attrs, attribute.String(AttrGenAIResponseID, data.ResponseID))
	}
	if data.FinishReason != "" {
		attrs = append(attrs, attribute.String(AttrGenAIResponseFinish, data.FinishReason))
	}
	if data.InputTokens > 0 {
		attrs = append(attrs, attribute.Int64(AttrGenAIUsageInputTokens, data.InputTokens))
	}
	if data.OutputTokens > 0 {
		attrs = append(attrs, attribute.Int64(AttrGenAIUsageOutputTokens, data.OutputTokens))
	}
	span.SetAttributes(attrs...)
}

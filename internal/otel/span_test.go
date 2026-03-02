package crabwiseotel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestEmitGenAISpan_Attributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(tracenoop.NewTracerProvider())

	data := GenAISpanData{
		System:        "openai",
		Operation:     "chat",
		RequestModel:  "gpt-4o",
		ResponseModel: "gpt-4o-2024-08-06",
		FinishReason:  "stop",
		InputTokens:   42,
		OutputTokens:  17,
		Outcome:       "success",
		Provider:      "openai",
		AdapterID:     "proxy",
	}

	EmitGenAISpan(context.Background(), data)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name() != "chat gpt-4o" {
		t.Fatalf("expected span name 'chat gpt-4o', got %q", span.Name())
	}
	if span.SpanKind() != trace.SpanKindClient {
		t.Fatalf("expected span kind CLIENT, got %v", span.SpanKind())
	}

	attrs := make(map[string]interface{})
	for _, a := range span.Attributes() {
		switch a.Value.Type() {
		case 1: // BOOL
			attrs[string(a.Key)] = a.Value.AsBool()
		case 2: // INT64
			attrs[string(a.Key)] = a.Value.AsInt64()
		case 3: // FLOAT64
			attrs[string(a.Key)] = a.Value.AsFloat64()
		case 4: // STRING
			attrs[string(a.Key)] = a.Value.AsString()
		}
	}

	assertStr := func(key, want string) {
		t.Helper()
		if got, ok := attrs[key]; !ok || got != want {
			t.Errorf("attr %s: want %q, got %v", key, want, got)
		}
	}
	assertInt := func(key string, want int64) {
		t.Helper()
		if got, ok := attrs[key]; !ok || got != want {
			t.Errorf("attr %s: want %d, got %v", key, want, got)
		}
	}

	assertStr(AttrGenAISystem, "openai")
	assertStr(AttrGenAIOperationName, "chat")
	assertStr(AttrGenAIRequestModel, "gpt-4o")
	assertStr(AttrGenAIResponseModel, "gpt-4o-2024-08-06")
	assertStr(AttrGenAIResponseFinish, "stop")
	assertStr(AttrCrabwiseOutcome, "success")
	assertStr(AttrCrabwiseProvider, "openai")
	assertStr(AttrCrabwiseAdapterID, "proxy")
	assertInt(AttrGenAIUsageInputTokens, 42)
	assertInt(AttrGenAIUsageOutputTokens, 17)
}

func TestEmitGenAISpan_OptionalFieldsOmitted(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(tracenoop.NewTracerProvider())

	EmitGenAISpan(context.Background(), GenAISpanData{
		System:       "openai",
		Operation:    "chat",
		RequestModel: "gpt-4o",
		Outcome:      "failure",
		Provider:     "openai",
		AdapterID:    "proxy",
	})

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	for _, a := range spans[0].Attributes() {
		key := string(a.Key)
		switch key {
		case AttrGenAIResponseModel, AttrGenAIResponseID, AttrGenAIResponseFinish,
			AttrGenAIUsageInputTokens, AttrGenAIUsageOutputTokens:
			t.Errorf("optional attribute %s should not be present when zero-valued", key)
		}
	}
}

func TestEmitGenAISpan_NoOpWhenDisabled(t *testing.T) {
	// Reset to default no-op provider
	otel.SetTracerProvider(tracenoop.NewTracerProvider())

	// Should not panic
	EmitGenAISpan(context.Background(), GenAISpanData{
		System:       "openai",
		Operation:    "chat",
		RequestModel: "gpt-4o",
		Outcome:      "success",
		Provider:     "openai",
	})
}

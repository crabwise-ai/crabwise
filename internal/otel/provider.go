package crabwiseotel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// Config controls OTel TracerProvider initialization.
type Config struct {
	Enabled        bool
	Endpoint       string // OTLP HTTP endpoint (e.g. "localhost:4318")
	ExportInterval time.Duration
	ServiceName    string
	ServiceVersion string
}

// Init creates and registers a TracerProvider. Returns a shutdown func.
// If cfg.Enabled is false, returns a no-op shutdown func (no global provider set).
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
	}
	// Default to insecure for local collectors
	if cfg.Endpoint == "" || strings.HasPrefix(cfg.Endpoint, "localhost") || strings.HasPrefix(cfg.Endpoint, "127.0.0.1") {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	batchOpts := []sdktrace.BatchSpanProcessorOption{}
	if cfg.ExportInterval > 0 {
		batchOpts = append(batchOpts, sdktrace.WithBatchTimeout(cfg.ExportInterval))
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, batchOpts...),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

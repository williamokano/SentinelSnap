package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"go.opentelemetry.io/otel/sdk/resource"
)

// newTracerProvider builds a TracerProvider according to cfg.TracesMode.
// Push mode: sends spans to cfg.OTLPEndpoint via batch processor.
// Off mode: returns a no-op provider (spans are still created but not exported).
func newTracerProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdktrace.TracerProvider, error) {
	if cfg.TracesMode == ModeOff {
		return sdktrace.NewTracerProvider(sdktrace.WithResource(res)), nil
	}

	var exp sdktrace.SpanExporter
	var err error
	if cfg.OTLPProtocol == "grpc" {
		exp, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpointURL(cfg.OTLPEndpoint),
		)
	} else {
		exp, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceSampleRate))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sampler),
	)
	return tp, nil
}

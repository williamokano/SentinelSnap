package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"go.opentelemetry.io/otel/sdk/resource"
)

// newTracerProvider builds an OTLP-push TracerProvider. Sampler uses
// TraceIDRatioBased for root spans; child spans honour the parent's sampling
// decision (ParentBased wrapper). Set TraceSampleRate=1.0 to always follow parent.
// Off mode is handled by the caller via a noop provider.
func newTracerProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdktrace.TracerProvider, error) {
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

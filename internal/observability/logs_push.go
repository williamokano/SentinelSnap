// This file imports the OTel logs SDK (sdk/log v0.x) whose API is not yet
// covered by the OpenTelemetry Go stability guarantee. Isolating it here limits
// the blast radius of a future breaking change.
package observability

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

// newLogProvider creates a log provider that pushes records to the OTLP endpoint.
func newLogProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdklog.LoggerProvider, error) {
	var exp sdklog.Exporter
	var err error
	if cfg.OTLPProtocol == "grpc" {
		exp, err = otlploggrpc.New(ctx,
			otlploggrpc.WithEndpointURL(cfg.OTLPEndpoint),
		)
	} else {
		exp, err = otlploghttp.New(ctx,
			otlploghttp.WithEndpointURL(cfg.OTLPEndpoint),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("otlp log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
	)
	return lp, nil
}

// newPushLogger returns an slog.Logger that ships records to lp via the OTel
// slog bridge. This replaces the stdout logger when LogsMode is "push"; no
// records are written to stdout in push mode. level is respected by wrapping
// the bridge in a `levelHandler` so only records at or above the configured
// threshold are forwarded.
func newPushLogger(lp *sdklog.LoggerProvider, level slog.Level) *slog.Logger {
	bridge := otelslog.NewHandler("sentinelsnap",
		otelslog.WithLoggerProvider(lp),
		otelslog.WithVersion(serviceVersion),
	)
	return slog.New(&traceHandler{Handler: &levelHandler{level: level, inner: bridge}})
}

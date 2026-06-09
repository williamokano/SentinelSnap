package observability

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

var serviceVersion = "dev"

func init() {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		serviceVersion = info.Main.Version
	}
}

func newResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		// ErrPartialResource and ErrSchemaURLConflict are non-fatal; the returned
		// resource is still usable. Only truly unexpected errors abort startup.
		if errors.Is(err, resource.ErrPartialResource) || errors.Is(err, resource.ErrSchemaURLConflict) {
			slog.Warn("otel resource detection partial, continuing", "error", err)
			return res, nil
		}
		return nil, err
	}
	return res, nil
}

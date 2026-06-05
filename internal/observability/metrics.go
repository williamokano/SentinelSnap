package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// photoSizeView overrides the default histogram boundaries for sentinelsnap.photo.size.
// The OTel default caps at ~10 KB, which puts all real photos in the +Inf bucket.
// These boundaries cover the typical range for compressed mobile photos up to 10 MB.
func photoSizeView() sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: "sentinelsnap.photo.size"},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{
					10_000, 50_000, 100_000, 500_000,
					1_000_000, 2_500_000, 5_000_000, 10_000_000,
				},
			},
		},
	)
}

// newMeterProvider builds a MeterProvider according to cfg.MetricsMode.
// Pull mode: returns a Prometheus exporter and a promhttp.Handler for the router.
// Push mode: returns an OTLP exporter sending to cfg.OTLPEndpoint.
// Off mode: returns a no-reader provider (instruments exist but emit nothing).
// The returned http.Handler is non-nil only in pull mode.
func newMeterProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdkmetric.MeterProvider, http.Handler, error) {
	switch cfg.MetricsMode {
	case ModePull:
		return newPrometheusMeterProvider(res)
	case ModePush:
		return newOTLPMeterProvider(ctx, res, cfg)
	case ModeOff:
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithView(photoSizeView()),
		)
		return mp, nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown OTEL_METRICS_MODE %q (valid: pull, push, off)", cfg.MetricsMode)
	}
}

func newPrometheusMeterProvider(res *resource.Resource) (*sdkmetric.MeterProvider, http.Handler, error) {
	reg := prometheus.NewRegistry()
	exp, err := prometheusexporter.New(
		prometheusexporter.WithRegisterer(reg),
		prometheusexporter.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("prometheus exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exp),
		sdkmetric.WithView(photoSizeView()),
	)
	h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
	return mp, h, nil
}

func newOTLPMeterProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdkmetric.MeterProvider, http.Handler, error) {
	var exp sdkmetric.Exporter
	var err error
	if cfg.OTLPProtocol == "grpc" {
		exp, err = otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpointURL(cfg.OTLPEndpoint),
		)
	} else {
		exp, err = otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpointURL(cfg.OTLPEndpoint),
		)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("otlp metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(15*time.Second))),
		sdkmetric.WithView(photoSizeView()),
	)
	return mp, nil, nil
}

package observability

import (
	"time"

	goruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/metric"
)

func startRuntimeMetrics(mp metric.MeterProvider) error {
	return goruntime.Start(
		goruntime.WithMinimumReadMemStatsInterval(10*time.Second),
		goruntime.WithMeterProvider(mp),
	)
}

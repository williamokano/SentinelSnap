package observability

import (
	"time"

	goruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
)

func startRuntimeMetrics() error {
	return goruntime.Start(
		goruntime.WithMinimumReadMemStatsInterval(10 * time.Second),
	)
}

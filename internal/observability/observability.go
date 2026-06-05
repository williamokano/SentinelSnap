package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"

	"github.com/williamokano/sentinelsnap/internal/config"
)

// Signal mode constants used by MetricsMode, TracesMode, and LogsMode.
const (
	ModeOff    = "off"
	ModePull   = "pull"
	ModePush   = "push"
	ModeStdout = "stdout"
)

// Config holds all observability configuration.
// Logging fields (LogLevel, LogFormat) are active in Phase 1.
// OTel exporter fields are populated from the application config and consumed
// by their respective providers (metrics in Phase 2, traces and log-push in Phase 3).
type Config struct {
	// Logging
	LogLevel  string
	LogFormat string

	// OTel exporter settings — endpoint, protocol, sampling, and per-signal modes.
	Enabled         bool
	ServiceName     string
	OTLPEndpoint    string
	OTLPProtocol    string
	MetricsMode     string
	TracesMode      string
	LogsMode        string
	TraceSampleRate float64
}

// Result carries the outputs of Setup that callers need to wire into the
// application (metrics handler for the router, custom domain instruments).
// Shutdown and WrapHandler are always non-nil. MetricsHandler is non-nil only
// in pull metrics mode. AppMetrics is always non-nil; its methods are no-ops
// when OTel is disabled.
type Result struct {
	// Shutdown flushes and stops all providers. Call it on graceful shutdown.
	Shutdown func(context.Context) error
	// MetricsHandler is non-nil when MetricsMode is "pull"; mount it at /metrics.
	MetricsHandler http.Handler
	// AppMetrics holds custom domain instruments. Always non-nil; methods no-op
	// when OTel is disabled.
	AppMetrics *AppMetrics
	// WrapHandler wraps h with OTel HTTP instrumentation. When OTel is disabled,
	// it returns h unchanged.
	WrapHandler func(h http.Handler) http.Handler
}

// Validate checks that mode strings and sample rate are within valid ranges.
// Setup calls this automatically; call it early in tests or CLIs that build
// Config directly.
func (c Config) Validate() error {
	valid := map[string]bool{ModeOff: true, ModePull: true, ModePush: true, ModeStdout: true}
	if !valid[c.MetricsMode] {
		return fmt.Errorf("invalid OTEL_METRICS_MODE %q (valid: off, pull, push)", c.MetricsMode)
	}
	if !valid[c.TracesMode] {
		return fmt.Errorf("invalid OTEL_TRACES_MODE %q (valid: off, push)", c.TracesMode)
	}
	if c.TraceSampleRate < 0 || c.TraceSampleRate > 1 {
		return fmt.Errorf("OTEL_TRACES_SAMPLER_ARG %v out of [0.0, 1.0]", c.TraceSampleRate)
	}
	return nil
}

// FromAppConfig builds an observability Config from the application config.
// This is the intended sole constructor; keep all field mappings here.
func FromAppConfig(cfg *config.Config) Config {
	return Config{
		LogLevel:        cfg.LogLevel,
		LogFormat:       cfg.LogFormat,
		Enabled:         cfg.OtelEnabled,
		ServiceName:     cfg.OtelServiceName,
		OTLPEndpoint:    cfg.OTLPEndpoint,
		OTLPProtocol:    cfg.OTLPProtocol,
		MetricsMode:     cfg.MetricsMode,
		TracesMode:      cfg.TracesMode,
		LogsMode:        cfg.LogsMode,
		TraceSampleRate: cfg.TraceSampleRate,
	}
}

// Setup initialises all enabled observability signals, installs providers as
// OTel globals, and sets the structured slog logger as the process default.
// Returns a Result with a shutdown func, an optional /metrics handler, and app
// metrics instruments. LOG_LEVEL/LOG_FORMAT are always applied; OTel metrics
// are enabled when OTEL_ENABLED=true.
func Setup(ctx context.Context, cfg Config) (Result, error) {
	logger, err := newLogger(cfg)
	if err != nil {
		return Result{}, err
	}
	slog.SetDefault(logger)

	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}

	if !cfg.Enabled {
		return Result{
			Shutdown:    func(context.Context) error { return nil },
			WrapHandler: func(h http.Handler) http.Handler { return h },
			AppMetrics:  &AppMetrics{},
		}, nil
	}

	// Route OTel-internal errors (e.g. OTLP export failures) to slog so they
	// appear in the structured log stream rather than going to os.Stderr.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Error("opentelemetry internal error", "error", err)
	}))

	res, err := newResource(ctx, cfg)
	if err != nil {
		return Result{}, err
	}

	mp, metricsHandler, err := newMeterProvider(ctx, res, cfg)
	if err != nil {
		return Result{}, err
	}
	otel.SetMeterProvider(mp)

	if err := startRuntimeMetrics(mp); err != nil {
		return Result{}, err
	}

	am, err := newAppMetrics(mp)
	if err != nil {
		return Result{}, err
	}

	return Result{
		Shutdown:       mp.Shutdown,
		MetricsHandler: metricsHandler,
		AppMetrics:     am,
		WrapHandler:    func(h http.Handler) http.Handler { return otelhttp.NewHandler(h, "sentinelsnap") },
	}, nil
}

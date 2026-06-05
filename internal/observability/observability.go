package observability

import (
	"context"
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
type Result struct {
	// Shutdown flushes and stops all providers. Call it on graceful shutdown.
	Shutdown func(context.Context) error
	// MetricsHandler is non-nil when MetricsMode is "pull"; mount it at /metrics.
	MetricsHandler http.Handler
	// AppMetrics holds custom domain instruments; always non-nil when OTEL_ENABLED.
	AppMetrics *AppMetrics
	// WrapHandler wraps h with OTel HTTP instrumentation. When OTel is disabled,
	// it returns h unchanged.
	WrapHandler func(h http.Handler) http.Handler
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

	if !cfg.Enabled {
		return Result{
			Shutdown:    func(context.Context) error { return nil },
			WrapHandler: func(h http.Handler) http.Handler { return h },
		}, nil
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return Result{}, err
	}

	mp, metricsHandler, err := newMeterProvider(ctx, res, cfg)
	if err != nil {
		return Result{}, err
	}
	otel.SetMeterProvider(mp)

	if err := startRuntimeMetrics(); err != nil {
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

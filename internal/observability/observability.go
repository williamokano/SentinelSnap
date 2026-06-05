package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

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
	validMetrics := map[string]bool{ModeOff: true, ModePull: true, ModePush: true}
	validTraces := map[string]bool{ModeOff: true, ModePush: true}
	validLogs := map[string]bool{ModeStdout: true, ModePush: true}
	if !validMetrics[c.MetricsMode] {
		return fmt.Errorf("invalid OTEL_METRICS_MODE %q (valid: off, pull, push)", c.MetricsMode)
	}
	if !validTraces[c.TracesMode] {
		return fmt.Errorf("invalid OTEL_TRACES_MODE %q (valid: off, push)", c.TracesMode)
	}
	if !validLogs[c.LogsMode] {
		return fmt.Errorf("invalid OTEL_LOGS_MODE %q (valid: stdout, push)", c.LogsMode)
	}
	if c.TraceSampleRate < 0 || c.TraceSampleRate > 1 {
		return fmt.Errorf("OTEL_TRACES_SAMPLER_ARG %v out of [0.0, 1.0]", c.TraceSampleRate)
	}
	if c.Enabled && c.OTLPEndpoint == "" && (c.MetricsMode == ModePush || c.TracesMode == ModePush || c.LogsMode == ModePush) {
		return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must be set when any signal uses push mode")
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
// metrics instruments. LOG_LEVEL/LOG_FORMAT are always applied; OTel signals
// are enabled when OTEL_ENABLED=true.
func Setup(ctx context.Context, cfg Config) (Result, error) {
	logger, level, err := newLogger(cfg)
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

	var tpShutdown func(context.Context) error
	if cfg.TracesMode == ModeOff {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
		tpShutdown = func(context.Context) error { return nil }
	} else {
		tp, err := newTracerProvider(ctx, res, cfg)
		if err != nil {
			return Result{}, err
		}
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		tpShutdown = tp.Shutdown
	}

	slog.Info("observability configured",
		"metrics_mode", cfg.MetricsMode,
		"traces_mode", cfg.TracesMode,
		"logs_mode", cfg.LogsMode,
	)

	shutdowns := []func(context.Context) error{mp.Shutdown, tpShutdown}

	if cfg.LogsMode == ModePush {
		lp, err := newLogProvider(ctx, res, cfg)
		if err != nil {
			return Result{}, err
		}
		slog.SetDefault(newPushLogger(lp, level))
		shutdowns = append(shutdowns, lp.Shutdown)
	}

	finalShutdowns := shutdowns
	shutdown := func(ctx context.Context) error {
		var errs []error
		for i := len(finalShutdowns) - 1; i >= 0; i-- {
			if err := finalShutdowns[i](ctx); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}

	return Result{
		Shutdown:       shutdown,
		MetricsHandler: metricsHandler,
		AppMetrics:     am,
		WrapHandler:    func(h http.Handler) http.Handler { return otelhttp.NewHandler(h, "sentinelsnap") },
	}, nil
}

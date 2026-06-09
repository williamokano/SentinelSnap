package observability

import (
	"context"
	"log/slog"

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
// OTel exporter fields are populated from the application config but not yet
// consumed by any provider; they become active in Phase 2+.
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

// Setup initialises observability signals from cfg, installs a structured slog
// logger as the process default, and returns a shutdown function that flushes
// and stops all providers. LOG_LEVEL and LOG_FORMAT are always applied.
// OTel metrics, traces, and log-push are not yet active (Phase 2+);
// cfg.Enabled and the exporter fields are accepted but currently unused.
func Setup(_ context.Context, cfg Config) (func(context.Context) error, error) {
	logger, err := newLogger(cfg)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	return func(_ context.Context) error { return nil }, nil
}

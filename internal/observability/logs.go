package observability

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

func newLogger(cfg Config) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return nil, fmt.Errorf("invalid LOG_LEVEL %q: %w", cfg.LogLevel, err)
	}
	opts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(cfg.LogFormat, "text") {
		return slog.New(slog.NewTextHandler(os.Stdout, opts)), nil
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts)), nil
}

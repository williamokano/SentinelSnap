package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

func newLogger(cfg Config) (*slog.Logger, slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return nil, 0, fmt.Errorf("invalid LOG_LEVEL %q: %w", cfg.LogLevel, err)
	}
	opts := &slog.HandlerOptions{Level: level}
	var base slog.Handler
	if strings.EqualFold(cfg.LogFormat, "text") {
		base = slog.NewTextHandler(os.Stdout, opts)
	} else {
		base = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(&traceHandler{Handler: base}), level, nil
}

// traceHandler wraps a slog.Handler and adds trace_id/span_id from the OTel
// span in context whenever a valid span is present.
type traceHandler struct {
	slog.Handler
}

func (h *traceHandler) Handle(ctx context.Context, rec slog.Record) error {
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
		rec.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, rec)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{Handler: h.Handler.WithGroup(name)}
}

// levelHandler filters log records to only those at or above the given level.
// Used to apply level filtering to handlers that don't natively support it
// (e.g. the otelslog bridge).
type levelHandler struct {
	level slog.Level
	inner slog.Handler
}

func (h *levelHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *levelHandler) Handle(ctx context.Context, rec slog.Record) error {
	return h.inner.Handle(ctx, rec)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{level: h.level, inner: h.inner.WithAttrs(attrs)}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{level: h.level, inner: h.inner.WithGroup(name)}
}

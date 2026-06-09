package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureHandler records log record attributes into a JSON buffer for assertions.
type captureHandler struct {
	buf *bytes.Buffer
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, rec slog.Record) error {
	m := map[string]string{"msg": rec.Message}
	rec.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	b, _ := json.Marshal(m)
	h.buf.Write(b)
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// notifyHandler records whether Handle was called.
type notifyHandler struct {
	called *bool
}

func (h *notifyHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *notifyHandler) Handle(_ context.Context, _ slog.Record) error {
	*h.called = true
	return nil
}
func (h *notifyHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *notifyHandler) WithGroup(string) slog.Handler      { return h }

func TestTraceHandler_InjectsTraceIDs(t *testing.T) {
	buf := &bytes.Buffer{}
	h := &traceHandler{Handler: &captureHandler{buf: buf}}

	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	require.NoError(t, h.Handle(ctx, rec))

	var m map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m))
	assert.NotEmpty(t, m["trace_id"], "trace_id must be injected when span is active")
	assert.NotEmpty(t, m["span_id"], "span_id must be injected when span is active")
	assert.NotEqual(t, "00000000000000000000000000000000", m["trace_id"])
	assert.NotEqual(t, "0000000000000000", m["span_id"])
}

func TestTraceHandler_NoSpan_PassesThrough(t *testing.T) {
	buf := &bytes.Buffer{}
	h := &traceHandler{Handler: &captureHandler{buf: buf}}

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "no span", 0)
	require.NoError(t, h.Handle(context.Background(), rec))

	var m map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &m))
	assert.NotContains(t, m, "trace_id", "trace_id must not appear when no span is active")
	assert.NotContains(t, m, "span_id", "span_id must not appear when no span is active")
}

func TestTraceHandler_WithAttrs_ReturnsTraceHandler(t *testing.T) {
	h := &traceHandler{Handler: slog.NewJSONHandler(&bytes.Buffer{}, nil)}
	h2 := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	_, ok := h2.(*traceHandler)
	assert.True(t, ok, "WithAttrs must return *traceHandler to preserve trace injection on derived loggers")
}

func TestTraceHandler_WithGroup_ReturnsTraceHandler(t *testing.T) {
	h := &traceHandler{Handler: slog.NewJSONHandler(&bytes.Buffer{}, nil)}
	h2 := h.WithGroup("grp")
	_, ok := h2.(*traceHandler)
	assert.True(t, ok, "WithGroup must return *traceHandler to preserve trace injection on derived loggers")
}

func TestLevelHandler_Enabled(t *testing.T) {
	h := &levelHandler{level: slog.LevelWarn, inner: slog.NewJSONHandler(&bytes.Buffer{}, nil)}
	ctx := context.Background()

	assert.False(t, h.Enabled(ctx, slog.LevelDebug))
	assert.False(t, h.Enabled(ctx, slog.LevelInfo))
	assert.True(t, h.Enabled(ctx, slog.LevelWarn))
	assert.True(t, h.Enabled(ctx, slog.LevelError))
}

func TestLevelHandler_Handle_SkipsBelowThreshold(t *testing.T) {
	var called bool
	h := &levelHandler{level: slog.LevelWarn, inner: &notifyHandler{called: &called}}

	rec := slog.NewRecord(time.Now(), slog.LevelDebug, "debug msg", 0)
	require.NoError(t, h.Handle(context.Background(), rec))
	assert.False(t, called, "inner Handle must not be called for below-threshold records")
}

func TestLevelHandler_WithAttrs_PreservesLevel(t *testing.T) {
	h := &levelHandler{level: slog.LevelError, inner: slog.NewJSONHandler(&bytes.Buffer{}, nil)}
	h2 := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	lh, ok := h2.(*levelHandler)
	require.True(t, ok, "WithAttrs must return *levelHandler")
	assert.Equal(t, slog.LevelError, lh.level, "level must be preserved after WithAttrs")
	assert.False(t, lh.Enabled(context.Background(), slog.LevelWarn))
}

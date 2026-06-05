package observability

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestSetup_DisabledPath(t *testing.T) {
	cfg := Config{
		LogLevel:    "info",
		LogFormat:   "json",
		Enabled:     false,
		MetricsMode: ModePull,
		TracesMode:  ModeOff,
		LogsMode:    ModeStdout,
	}

	res, err := Setup(context.Background(), cfg)
	require.NoError(t, err)

	assert.NotNil(t, res.Shutdown)
	assert.NotNil(t, res.WrapHandler)
	assert.NotNil(t, res.AppMetrics)
	assert.Nil(t, res.MetricsHandler)

	// Shutdown must not error.
	assert.NoError(t, res.Shutdown(context.Background()))

	// WrapHandler must return the handler unchanged.
	handler := &dummyHandler{}
	wrapped := res.WrapHandler(handler)
	assert.Equal(t, handler, wrapped)
}

func TestSetup_InvalidLogLevel(t *testing.T) {
	cfg := Config{
		LogLevel:    "verbose",
		LogFormat:   "json",
		Enabled:     false,
		MetricsMode: ModePull,
		TracesMode:  ModeOff,
		LogsMode:    ModeStdout,
	}
	_, err := Setup(context.Background(), cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
}

func TestConfigValidate(t *testing.T) {
	base := Config{
		LogLevel:        "info",
		LogFormat:       "json",
		MetricsMode:     ModePull,
		TracesMode:      ModeOff,
		LogsMode:        ModeStdout,
		TraceSampleRate: 1.0,
	}

	t.Run("valid", func(t *testing.T) {
		assert.NoError(t, base.Validate())
	})
	t.Run("invalid MetricsMode", func(t *testing.T) {
		c := base
		c.MetricsMode = "banana"
		assert.ErrorContains(t, c.Validate(), "OTEL_METRICS_MODE")
	})
	t.Run("invalid TracesMode", func(t *testing.T) {
		c := base
		c.TracesMode = "scrape"
		assert.ErrorContains(t, c.Validate(), "OTEL_TRACES_MODE")
	})
	t.Run("invalid LogsMode", func(t *testing.T) {
		c := base
		c.LogsMode = "scrape"
		assert.ErrorContains(t, c.Validate(), "OTEL_LOGS_MODE")
	})
	t.Run("sample rate below 0", func(t *testing.T) {
		c := base
		c.TraceSampleRate = -0.1
		assert.ErrorContains(t, c.Validate(), "OTEL_TRACES_SAMPLER_ARG")
	})
	t.Run("sample rate above 1", func(t *testing.T) {
		c := base
		c.TraceSampleRate = 1.1
		assert.ErrorContains(t, c.Validate(), "OTEL_TRACES_SAMPLER_ARG")
	})
}

func TestAppMetrics_NilSafe(t *testing.T) {
	ctx := context.Background()
	var am *AppMetrics

	// None of these should panic.
	assert.NotPanics(t, func() { am.SnapCreated(ctx) })
	assert.NotPanics(t, func() { am.PhotoStored(ctx, 1024) })
	assert.NotPanics(t, func() { am.SSEBroadcast(ctx, "test") })
	assert.NotPanics(t, func() { _ = am.SetSSEClientsFn(func() int64 { return 0 }) })
}

func TestAppMetrics_ZeroValueSafe(t *testing.T) {
	ctx := context.Background()
	am := &AppMetrics{} // zero value, all instruments nil

	assert.NotPanics(t, func() { am.SnapCreated(ctx) })
	assert.NotPanics(t, func() { am.PhotoStored(ctx, 512) })
	assert.NotPanics(t, func() { am.SSEBroadcast(ctx, "ping") })
	assert.NoError(t, am.SetSSEClientsFn(func() int64 { return 5 }))
}

func TestAppMetrics_SetSSEClientsFn_DoubleRegister(t *testing.T) {
	mp := sdkmetric.NewMeterProvider()
	defer mp.Shutdown(context.Background()) //nolint:errcheck
	am, err := newAppMetrics(mp)
	require.NoError(t, err)
	assert.NoError(t, am.SetSSEClientsFn(func() int64 { return 1 }))
	err = am.SetSSEClientsFn(func() int64 { return 2 })
	assert.Error(t, err)
	assert.ErrorContains(t, err, "more than once")
}

func TestSetup_EnabledPullMetrics(t *testing.T) {
	cfg := Config{
		LogLevel:        "info",
		LogFormat:       "json",
		Enabled:         true,
		ServiceName:     "test-svc",
		MetricsMode:     ModePull,
		TracesMode:      ModeOff,
		LogsMode:        ModeStdout,
		TraceSampleRate: 1.0,
	}
	res, err := Setup(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, res.MetricsHandler)
	assert.NotNil(t, res.AppMetrics)
	require.NoError(t, res.Shutdown(context.Background()))
}

// dummyHandler is a minimal http.Handler for use in tests.
type dummyHandler struct{}

func (d *dummyHandler) ServeHTTP(_ http.ResponseWriter, _ *http.Request) {}

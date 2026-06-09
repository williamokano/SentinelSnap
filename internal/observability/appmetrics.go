package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// AppMetrics holds all custom domain instruments. The zero value is safe to use
// (all methods check for nil instruments and silently no-op), so callers do not
// need to guard against disabled metrics.
type AppMetrics struct {
	meter                 metric.Meter // retained solely for RegisterCallback in SetSSEClientsFn
	snapsCreated          metric.Int64Counter
	photosStored          metric.Int64Counter
	photosBytesIn         metric.Int64Counter
	photoSize             metric.Int64Histogram
	sseClientsGauge       metric.Int64ObservableGauge
	sseBroadcasts         metric.Int64Counter
	sseCallbackRegistered bool
}

// newAppMetrics registers all custom instruments on mp.
func newAppMetrics(mp metric.MeterProvider) (*AppMetrics, error) {
	m := mp.Meter("sentinelsnap/app")
	am := &AppMetrics{meter: m}
	var err error

	if am.snapsCreated, err = m.Int64Counter(
		"sentinelsnap.snaps.created",
		metric.WithDescription("Total number of snaps created"),
		metric.WithUnit("{snap}"),
	); err != nil {
		return nil, err
	}

	if am.photosStored, err = m.Int64Counter(
		"sentinelsnap.photos.stored",
		metric.WithDescription("Total number of photos stored"),
		metric.WithUnit("{photo}"),
	); err != nil {
		return nil, err
	}

	if am.photosBytesIn, err = m.Int64Counter(
		"sentinelsnap.photos.bytes.stored",
		metric.WithDescription("Total bytes of photo data stored"),
		metric.WithUnit("By"),
	); err != nil {
		return nil, err
	}

	if am.photoSize, err = m.Int64Histogram(
		"sentinelsnap.photo.size",
		metric.WithDescription("Size distribution of individual uploaded photos"),
		metric.WithUnit("By"),
	); err != nil {
		return nil, err
	}

	if am.sseBroadcasts, err = m.Int64Counter(
		"sentinelsnap.sse.broadcasts",
		metric.WithDescription("Total number of SSE broadcast events"),
		metric.WithUnit("{broadcast}"),
	); err != nil {
		return nil, err
	}

	if am.sseClientsGauge, err = m.Int64ObservableGauge(
		"sentinelsnap.sse.clients",
		metric.WithDescription("Number of currently connected SSE clients"),
		metric.WithUnit("{client}"),
	); err != nil {
		return nil, err
	}
	return am, nil
}

// SnapCreated increments the snaps.created counter.
func (a *AppMetrics) SnapCreated(ctx context.Context) {
	if a == nil || a.snapsCreated == nil {
		return
	}
	a.snapsCreated.Add(ctx, 1)
}

// PhotoStored increments the photos.stored counter and records byte counts.
func (a *AppMetrics) PhotoStored(ctx context.Context, sizeBytes int64) {
	if a == nil || a.photosStored == nil {
		return
	}
	a.photosStored.Add(ctx, 1)
	a.photosBytesIn.Add(ctx, sizeBytes)
	a.photoSize.Record(ctx, sizeBytes)
}

// sseEventOptions precomputes the measurement options for the event types the
// hub broadcasts, so the hot broadcast path does not rebuild an attribute set
// per call. Unknown event types fall back to per-call construction.
var sseEventOptions = func() map[string]metric.MeasurementOption {
	opts := make(map[string]metric.MeasurementOption)
	for _, et := range []string{"snap", "snap_updated", "snap_deleted"} {
		opts[et] = metric.WithAttributeSet(attribute.NewSet(attribute.String("event_type", et)))
	}
	return opts
}()

// SSEBroadcast increments the sse.broadcasts counter with the given event type.
func (a *AppMetrics) SSEBroadcast(ctx context.Context, eventType string) {
	if a == nil || a.sseBroadcasts == nil {
		return
	}
	opt, ok := sseEventOptions[eventType]
	if !ok {
		opt = metric.WithAttributes(attribute.String("event_type", eventType))
	}
	a.sseBroadcasts.Add(ctx, 1, opt)
}

// SetSSEClientsFn registers fn as the source for the sse.clients observable gauge.
// Call this once the SSE hub is ready; calling it more than once returns an error.
func (a *AppMetrics) SetSSEClientsFn(fn func() int64) error {
	if a == nil || a.sseClientsGauge == nil {
		return nil
	}
	if a.sseCallbackRegistered {
		return fmt.Errorf("observability: SetSSEClientsFn called more than once")
	}
	_, err := a.meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(a.sseClientsGauge, fn())
		return nil
	}, a.sseClientsGauge)
	if err == nil {
		a.sseCallbackRegistered = true
	}
	return err
}

package router

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/williamokano/sentinelsnap/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

const (
	cspValue = "default-src 'self'; " +
		"img-src 'self' data: https://unpkg.com https://*.tile.openstreetmap.org https://*.basemaps.cartocdn.com https://server.arcgisonline.com; " +
		"script-src 'self' https://unpkg.com; " +
		"style-src 'self' https://unpkg.com"
)

// securityHeaders returns a middleware that sets security-relevant HTTP response headers.
// HSTS is only set when cfg.HTTPSEnabled is true to avoid breaking HTTP-only deployments.
func securityHeaders(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Content-Security-Policy", cspValue)
			w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
			if cfg.HTTPSEnabled {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// routePattern returns the matched chi route pattern, or "" when unavailable.
func routePattern(ctx context.Context) string {
	if rctx := chi.RouteContext(ctx); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}

// routeTagger propagates the matched chi route pattern to the otelhttp labeler
// so that HTTP metrics carry a populated http.route attribute.
func routeTagger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if pattern := routePattern(ctx); pattern != "" {
			if labeler, ok := otelhttp.LabelerFromContext(ctx); ok {
				labeler.Add(semconv.HTTPRoute(pattern))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func slogRequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		reqID := middleware.GetReqID(ctx)
		t := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			route := r.URL.Path
			if p := routePattern(ctx); p != "" {
				route = p
			}
			logFn := slog.InfoContext
			if status >= 500 {
				logFn = slog.ErrorContext
			} else if status >= 400 {
				logFn = slog.WarnContext
			}
			logFn(ctx, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"route", route,
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(t).Milliseconds(),
				"request_id", reqID,
				"remote_addr", r.RemoteAddr,
			)
		}()
		next.ServeHTTP(ww, r)
	})
}

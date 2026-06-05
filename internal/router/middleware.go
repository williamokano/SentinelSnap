package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/williamokano/sentinelsnap/internal/config"
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
			logFn := slog.InfoContext
			if status >= 500 {
				logFn = slog.ErrorContext
			} else if status >= 400 {
				logFn = slog.WarnContext
			}
			logFn(ctx, "request",
				"method", r.Method,
				"path", r.URL.Path,
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

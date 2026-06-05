package router

import (
	"net/http"

	"github.com/williamokano/sentinelsnap/internal/config"
)

const (
	cspValue = "default-src 'self'; " +
		"img-src 'self' data: https://*.tile.openstreetmap.org https://*.basemaps.cartocdn.com https://server.arcgisonline.com; " +
		"script-src 'self' unpkg.com; " +
		"style-src 'self' unpkg.com"
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

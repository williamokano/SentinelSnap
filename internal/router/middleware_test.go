package router

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/williamokano/sentinelsnap/internal/config"
)

func TestSecurityHeaders(t *testing.T) {
	tests := []struct {
		name         string
		httpsEnabled bool
		wantHSTS     bool
	}{
		{"http mode - no HSTS", false, false},
		{"https mode - HSTS set", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{HTTPSEnabled: tt.httpsEnabled}
			handler := securityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
			assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
			assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "https://unpkg.com")
			assert.NotEmpty(t, rec.Header().Get("Permissions-Policy"))

			if tt.wantHSTS {
				assert.NotEmpty(t, rec.Header().Get("Strict-Transport-Security"))
			} else {
				assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
			}
		})
	}
}

func TestSlogRequestLogger_PanicLoggedAs500(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Same composition as router.New: logger outside, Recoverer inside, so the
	// 500 written by Recoverer is what the request log reports.
	h := slogRequestLogger(middleware.Recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/panic", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, buf.String(), `"status":500`)
}

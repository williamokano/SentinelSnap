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

func TestSlogRequestLogger_StatusCodes(t *testing.T) {
	tests := []struct {
		name           string
		handlerStatus  int
		writeBody      bool
		expectedStatus int
	}{
		{"200 OK", http.StatusOK, true, http.StatusOK},
		{"400 Bad Request", http.StatusBadRequest, true, http.StatusBadRequest},
		{"500 Internal Error", http.StatusInternalServerError, true, http.StatusInternalServerError},
		{"no write defaults to 200", 0, false, http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
			slog.SetDefault(logger)

			handler := slogRequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.writeBody {
					w.WriteHeader(tc.handlerStatus)
				}
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			// no chi context attached — tests the plain request path
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			logOutput := logBuf.String()
			assert.Contains(t, logOutput, "request")
		})
	}
}

func TestSlogRequestLogger_NoWriteDefaultsTo200(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	slog.SetDefault(logger)

	handler := slogRequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// write nothing — status should default to 200
	}))

	req := httptest.NewRequest(http.MethodGet, "/silent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Contains(t, logBuf.String(), `"status":200`)
}

func TestRouteTagger_NilChiContext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := routeTagger(next)

	// Request with no chi route context attached — should not panic.
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(w, req)
	})
	assert.True(t, called, "next handler should still be called")
}

package router

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/williamokano/sentinelsnap/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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

func TestRequireAuth(t *testing.T) {
	tests := []struct {
		name       string
		configured string // empty = auth disabled, middleware not installed
		authHeader string
		query      string
		wantStatus int
	}{
		{"no token configured - no credentials pass", "", "", "", http.StatusOK},
		{"no token configured - stale credentials still pass", "", "Bearer whatever", "", http.StatusOK},
		{"token configured - no credentials rejected", "s3cret", "", "", http.StatusUnauthorized},
		{"token configured - wrong bearer token rejected", "s3cret", "Bearer wrong", "", http.StatusUnauthorized},
		{"token configured - malformed authorization header rejected", "s3cret", "s3cret", "", http.StatusUnauthorized},
		{"token configured - wrong query token rejected", "s3cret", "", "?token=wrong", http.StatusUnauthorized},
		{"token configured - correct bearer token passes", "s3cret", "Bearer s3cret", "", http.StatusOK},
		{"token configured - correct query token passes (SSE)", "s3cret", "", "?token=s3cret", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			// Mirror router.New: the middleware is only installed when a
			// token is configured; otherwise the server stays fully open.
			var handler http.Handler = next
			if tc.configured != "" {
				handler = requireAuth(tc.configured)(next)
			}

			req := httptest.NewRequest(http.MethodGet, "/snaps"+tc.query, nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus == http.StatusUnauthorized {
				assert.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				assert.JSONEq(t, `{"error":"unauthorized"}`, rec.Body.String())
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

func TestDebugBodyLogger_Truncation(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantTruncated bool
	}{
		{"small body logged in full", "hello world", false},
		{"exactly 1 KiB not marked truncated", strings.Repeat("a", debugBodyLogPrefixBytes), false},
		{"large body truncated to 1 KiB", strings.Repeat("b", 5000), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			var seenByHandler string
			h := debugBodyLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				seenByHandler = string(b)
			}))

			req := httptest.NewRequest(http.MethodPost, "/snaps", strings.NewReader(tc.body))
			h.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tc.body, seenByHandler, "handler must still see the full body")

			logOutput := buf.String()
			if tc.wantTruncated {
				assert.Contains(t, logOutput, "(truncated)")
				assert.NotContains(t, logOutput, strings.Repeat("b", debugBodyLogPrefixBytes+1),
					"log must not contain more than the prefix")
			} else {
				assert.Contains(t, logOutput, tc.body)
				assert.NotContains(t, logOutput, "(truncated)")
			}
		})
	}
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

func TestRouteTagger_InjectsRouteLabel(t *testing.T) {
	// routeTagger must run inside a chi Group (after route matching) so that
	// rctx.RoutePattern() is already populated. Mirror the actual router.go setup.
	r := chi.NewRouter()
	var capturedLabeler *otelhttp.Labeler
	r.Group(func(r chi.Router) {
		r.Use(routeTagger)
		r.Get("/snaps/{id}", func(w http.ResponseWriter, r *http.Request) {
			l, ok := otelhttp.LabelerFromContext(r.Context())
			if ok {
				capturedLabeler = l
			}
		})
	})

	wrapped := otelhttp.NewHandler(r, "test")
	req := httptest.NewRequest(http.MethodGet, "/snaps/42", nil)
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	require.NotNil(t, capturedLabeler, "otelhttp labeler not found in context")
	var routeLabel string
	for _, a := range capturedLabeler.Get() {
		if string(a.Key) == "http.route" {
			routeLabel = a.Value.AsString()
		}
	}
	assert.Equal(t, "/snaps/{id}", routeLabel)
}

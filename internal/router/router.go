package router

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/handler"
	"github.com/williamokano/sentinelsnap/internal/hub"
)

//go:embed static
var staticFiles embed.FS

// New builds the HTTP router. metricsHandler, when non-nil, is mounted at /metrics
// outside the logging group so scraper traffic does not pollute request logs.
func New(cfg *config.Config, h *handler.SnapHandler, ev *hub.Hub, hh *handler.HealthHandler, metricsHandler http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(securityHeaders(cfg))

	// Register healthz before logging middlewares so health checks don't spam logs.
	r.Get("/healthz", hh.Check)

	if metricsHandler != nil {
		r.Handle("/metrics", metricsHandler)
	}

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		// Should never happen: the "static" directory is verified at compile time
		// by the //go:embed directive. Fail fast rather than serving 500s at runtime.
		panic(fmt.Sprintf("router: embedded static FS unavailable: %v", err))
	}

	r.Group(func(r chi.Router) {
		// Order matters: RequestID before the logger so request_id is loggable,
		// and Recoverer inside the logger so the 500 it writes on panic is the
		// status the logger reports (otherwise panics are logged as 200).
		r.Use(middleware.RequestID)
		r.Use(slogRequestLogger)
		r.Use(middleware.Recoverer)
		r.Use(routeTagger)
		if cfg.Debug {
			r.Use(debugBodyLogger)
		}

		r.Post("/snaps", h.CreateSnap)
		r.Get("/snaps", h.ListSnaps)
		r.Patch("/snaps/{id}", h.UpdateSnap)
		r.Delete("/snaps/{id}", h.DeleteSnap)
		r.Get("/photos/{token}", h.ServePhoto)
		r.Get("/events", ev.ServeSSE)

		if cfg.StorageBackend == "local" {
			fileServer := http.FileServer(http.Dir(cfg.LocalUploadDir))
			r.Handle("/uploads/*", http.StripPrefix("/uploads/", fileServer))
		}

		r.Handle("/*", http.FileServer(http.FS(staticFS)))
	})

	return r
}

func debugBodyLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			slog.DebugContext(r.Context(), "debug: failed to read body",
				"method", r.Method, "path", r.URL.Path, "error", err)
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		slog.DebugContext(r.Context(), "debug: request body",
			"method", r.Method, "path", r.URL.Path, "body", string(body))
		next.ServeHTTP(w, r)
	})
}

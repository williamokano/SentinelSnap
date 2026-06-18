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

		// Token-protected endpoints: everything that creates, mutates, lists,
		// or streams snap data. Auth is opt-in — with no API_TOKEN configured
		// the middleware is not installed and behavior is unchanged.
		r.Group(func(r chi.Router) {
			if cfg.APIToken != "" {
				r.Use(requireAuth(cfg.APIToken))
			}
			r.Post("/snaps", h.CreateSnap)
			r.Get("/snaps", h.ListSnaps)
			r.Patch("/snaps/{id}", h.UpdateSnap)
			r.Delete("/snaps/{id}", h.DeleteSnap)

			// Photos: simple GPS-less uploads, the feed listing of all
			// photos, and per-photo delete. Delete shares the /photos/{token}
			// path that serves the image — same capability URL, just DELETE.
			r.Post("/photos", h.CreatePhotos)
			r.Get("/photos", h.ListPhotos)
			r.Delete("/photos/{token}", h.DeletePhoto)

			r.Get("/events", ev.ServeSSE)
		})

		// Deliberately unauthenticated:
		//   - /photos/{token}: already capability URLs — each photo is reachable
		//     only via an unguessable per-photo token, and <img> tags cannot
		//     send Authorization headers anyway.
		//   - static files: the map page itself is not secret; the snap data
		//     it loads is what the token protects.
		// (/healthz and /metrics live outside this group: orchestrator health
		// checks and the metrics scraper must keep working without credentials.)
		r.Get("/photos/{token}", h.ServePhoto)
		// Clean URL for the photo feed page; the file itself also remains
		// reachable at /feed.html via the static file server below.
		r.Get("/feed", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, staticFS, "feed.html")
		})
		r.Handle("/*", http.FileServer(http.FS(staticFS)))
	})

	return r
}

// debugBodyLogPrefixBytes bounds how much of a request body the debug logger
// reads and logs; bodies with base64 photos run to megabytes per request.
const debugBodyLogPrefixBytes = 1024

func debugBodyLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read one byte past the prefix so we can tell whether the body was
		// actually truncated when it is exactly debugBodyLogPrefixBytes long.
		read := make([]byte, debugBodyLogPrefixBytes+1)
		n, err := io.ReadFull(r.Body, read)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			slog.DebugContext(r.Context(), "debug: failed to read body",
				"method", r.Method, "path", r.URL.Path, "error", err)
			next.ServeHTTP(w, r)
			return
		}
		read = read[:n]

		logged := string(read[:min(n, debugBodyLogPrefixBytes)])
		if n > debugBodyLogPrefixBytes {
			logged += " (truncated)"
		}
		slog.DebugContext(r.Context(), "debug: request body",
			"method", r.Method, "path", r.URL.Path, "body", logged)

		r.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(read), r.Body), r.Body}
		next.ServeHTTP(w, r)
	})
}

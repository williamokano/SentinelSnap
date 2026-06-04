package router

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/handler"
)

//go:embed static
var staticFiles embed.FS

func New(cfg *config.Config, h *handler.SnapHandler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	if cfg.Debug {
		r.Use(debugBodyLogger)
	}

	r.Post("/snaps", h.CreateSnap)
	r.Get("/snaps", h.ListSnaps)
	r.Delete("/snaps/{id}", h.DeleteSnap)

	if cfg.StorageBackend == "local" {
		fileServer := http.FileServer(http.Dir(cfg.LocalUploadDir))
		r.Handle("/uploads/*", http.StripPrefix("/uploads/", fileServer))
	}

	staticFS, _ := fs.Sub(staticFiles, "static")
	r.Handle("/*", http.FileServer(http.FS(staticFS)))

	return r
}

func debugBodyLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("[DEBUG] %s %s — failed to read body: %v", r.Method, r.URL.Path, err)
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		log.Printf("[DEBUG] %s %s — body: %s", r.Method, r.URL.Path, body)
		next.ServeHTTP(w, r)
	})
}

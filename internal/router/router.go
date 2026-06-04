package router

import (
	"embed"
	"io/fs"
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

	r.Post("/snaps", h.CreateSnap)
	r.Get("/snaps", h.ListSnaps)

	if cfg.StorageBackend == "local" {
		fileServer := http.FileServer(http.Dir(cfg.LocalUploadDir))
		r.Handle("/uploads/*", http.StripPrefix("/uploads/", fileServer))
	}

	staticFS, _ := fs.Sub(staticFiles, "static")
	r.Handle("/*", http.FileServer(http.FS(staticFS)))

	return r
}

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/handler"
	"github.com/williamokano/sentinelsnap/internal/hub"
	"github.com/williamokano/sentinelsnap/internal/migrate"
	"github.com/williamokano/sentinelsnap/internal/repository/postgres"
	"github.com/williamokano/sentinelsnap/internal/router"
	"github.com/williamokano/sentinelsnap/internal/storage"
	"github.com/williamokano/sentinelsnap/internal/storage/local"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := sqlx.Connect(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer db.Close()

	if err := migrate.Run(db); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	repo := postgres.New(db)

	var store storage.StorageProvider
	switch cfg.StorageBackend {
	case "local":
		store, err = local.New(cfg.LocalUploadDir)
		if err != nil {
			log.Fatalf("local storage: %v", err)
		}
	default:
		log.Fatalf("unknown storage backend: %q (supported: local)", cfg.StorageBackend)
	}

	ev := hub.New()
	h := handler.NewSnapHandler(repo, store, ev, cfg)
	hh := handler.NewHealthHandler(db)
	r := router.New(cfg, h, ev, hh)

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	timeout := time.Duration(cfg.ShutdownTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	ev.Close()
	log.Println("server stopped")
}

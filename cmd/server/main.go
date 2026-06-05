package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/XSAM/otelsql"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"

	"github.com/williamokano/sentinelsnap/internal/config"
	"github.com/williamokano/sentinelsnap/internal/handler"
	"github.com/williamokano/sentinelsnap/internal/hub"
	"github.com/williamokano/sentinelsnap/internal/migrate"
	"github.com/williamokano/sentinelsnap/internal/observability"
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

	ctx := context.Background()
	obs, err := observability.Setup(ctx, observability.FromAppConfig(cfg))
	if err != nil {
		log.Fatalf("observability: %v", err)
	}
	defer func() {
		if err := obs.Shutdown(ctx); err != nil {
			slog.Error("observability shutdown", "error", err)
		}
	}()

	// otelsql wraps the driver to emit DB spans and pool metrics.
	// The sqlx layer must use the original driver name ("postgres") so that
	// sqlx keeps $N-style bind vars instead of switching to positional ?.
	stdDB, err := otelsql.Open(cfg.DBDriver, cfg.DBDSN,
		otelsql.WithAttributes(semconv.DBSystemNamePostgreSQL),
	)
	if err != nil {
		slog.Error("open db", "error", err)
		os.Exit(1)
	}
	db := sqlx.NewDb(stdDB, "postgres")
	if err := db.PingContext(ctx); err != nil {
		slog.Error("ping db", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if _, err := otelsql.RegisterDBStatsMetrics(stdDB,
		otelsql.WithAttributes(semconv.DBSystemNamePostgreSQL),
	); err != nil {
		slog.Error("db stats metrics", "error", err)
		os.Exit(1)
	}

	if err := migrate.Run(db); err != nil {
		slog.Error("migrations", "error", err)
		os.Exit(1)
	}

	repo := postgres.New(db)

	var store storage.StorageProvider
	switch cfg.StorageBackend {
	case "local":
		store, err = local.New(cfg.LocalUploadDir)
		if err != nil {
			slog.Error("local storage", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("unknown storage backend", "backend", cfg.StorageBackend)
		os.Exit(1)
	}

	ev := hub.New()
	h := handler.NewSnapHandler(repo, store, ev, cfg, obs.AppMetrics)
	hh := handler.NewHealthHandler(db)
	r := router.New(cfg, h, ev, hh, obs.MetricsHandler)

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           obs.WrapHandler(r),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("serve", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down", "timeout_seconds", cfg.ShutdownTimeoutSeconds)

	timeout := time.Duration(cfg.ShutdownTimeoutSeconds) * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	ev.Close()
	slog.Info("server stopped")
}

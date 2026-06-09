package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	obs, err := observability.Setup(ctx, observability.FromAppConfig(cfg))
	if err != nil {
		log.Fatalf("observability: %v", err)
	}

	// observability.OpenDB wraps the driver with otelsql (DB spans + pool
	// metrics) when OTel is enabled, and hands back the matching cleanup.
	// sqlx.NewDb is always given "postgres" so that sqlx keeps $N bindvar style ($1, $2, …).
	stdDB, dbCleanup, err := observability.OpenDB(cfg.DBDriver, cfg.DBDSN, cfg.OtelEnabled)
	if err != nil {
		slog.Error("open db", "driver", cfg.DBDriver, "error", err)
		os.Exit(1)
	}
	defer dbCleanup()
	db := sqlx.NewDb(stdDB, "postgres")
	// Deferred (not run inline at the end of main) so the DB still closes if
	// anything in the shutdown sequence panics.
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("db close", "error", err)
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		slog.Error("ping db", "driver", cfg.DBDriver, "error", err)
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

	ev := hub.New(obs.AppMetrics)
	if err := obs.AppMetrics.SetSSEClientsFn(ev.ClientCount); err != nil {
		slog.Error("sse clients gauge", "error", err)
		os.Exit(1)
	}
	h := handler.NewSnapHandler(repo, store, ev, cfg, obs.AppMetrics)
	hh := handler.NewHealthHandler(db)
	r := router.New(cfg, h, ev, hh, obs.MetricsHandler)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           obs.WrapHandler(r),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Close SSE client channels as part of Shutdown: their handlers only
	// return when the hub channel closes, so without this every shutdown
	// with a connected browser blocks for the full timeout.
	srv.RegisterOnShutdown(ev.Close)

	slog.Info("listening", "addr", srv.Addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSeconds)*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown", "error", err)
	}
	// Use stdlib log: slog may route through the OTLP push logger whose
	// provider is about to be (or already is) shut down.
	if err := obs.Shutdown(shutdownCtx); err != nil {
		log.Printf("observability shutdown: %v", err)
	}
	log.Printf("server stopped")
}

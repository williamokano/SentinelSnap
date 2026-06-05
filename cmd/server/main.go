package main

import (
	"context"
	"database/sql"
	"errors"
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
	"go.opentelemetry.io/otel/attribute"

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

	// When OTel is enabled, wrap the driver with otelsql for DB span instrumentation
	// and pool metrics. When disabled, use the plain driver to avoid overhead.
	// sqlx.NewDb is always given "postgres" so that sqlx keeps $N bindvar style ($1, $2, …).
	var stdDB *sql.DB
	if cfg.OtelEnabled {
		stdDB, err = otelsql.Open(cfg.DBDriver, cfg.DBDSN,
			otelsql.WithAttributes(attribute.String("db.system.name", "postgresql")),
		)
	} else {
		stdDB, err = sql.Open(cfg.DBDriver, cfg.DBDSN)
	}
	if err != nil {
		slog.Error("open db", "driver", cfg.DBDriver, "error", err)
		os.Exit(1)
	}
	db := sqlx.NewDb(stdDB, "postgres")
	if err := db.PingContext(ctx); err != nil {
		slog.Error("ping db", "driver", cfg.DBDriver, "error", err)
		os.Exit(1)
	}

	var dbStatsReg interface{ Unregister() error }
	if cfg.OtelEnabled {
		dbStatsReg, err = otelsql.RegisterDBStatsMetrics(stdDB,
			otelsql.WithAttributes(attribute.String("db.system.name", "postgresql")),
		)
		if err != nil {
			slog.Error("db stats metrics", "error", err)
			os.Exit(1)
		}
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
	if dbStatsReg != nil {
		if err := dbStatsReg.Unregister(); err != nil {
			slog.Error("db stats unregister", "error", err)
		}
	}
	if err := db.Close(); err != nil {
		slog.Error("db close", "error", err)
	}
	ev.Close()
	// Use stdlib log: slog may route through the OTLP push logger whose
	// provider is about to be (or already is) shut down.
	if err := obs.Shutdown(shutdownCtx); err != nil {
		log.Printf("observability shutdown: %v", err)
	}
	log.Printf("server stopped")
}

package observability

import (
	"database/sql"
	"log/slog"

	"github.com/XSAM/otelsql"
	"go.opentelemetry.io/otel/attribute"
)

// OpenDB opens the database, wrapping the driver with otelsql when enabled so
// queries produce DB spans and the pool reports stats metrics. The returned
// cleanup releases the DB stats registration; it never closes the DB and is
// safe to run via defer even after the meter provider has shut down.
func OpenDB(driver, dsn string, enabled bool) (*sql.DB, func(), error) {
	if !enabled {
		db, err := sql.Open(driver, dsn)
		return db, func() {}, err
	}

	attrs := otelsql.WithAttributes(attribute.String("db.system.name", "postgresql"))
	db, err := otelsql.Open(driver, dsn, attrs)
	if err != nil {
		return nil, nil, err
	}
	reg, err := otelsql.RegisterDBStatsMetrics(db, attrs)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	cleanup := func() {
		if err := reg.Unregister(); err != nil {
			slog.Error("db stats unregister", "error", err)
		}
	}
	return db, cleanup, nil
}

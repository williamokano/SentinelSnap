// Package migrate applies SQL migrations from an fs.FS (normally the
// embedded migrations.FS) at startup.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

// advisoryLockKey is the application-wide pg_advisory_lock key that
// serializes migration runs across concurrent app instances. The value is
// arbitrary (0x53454E54 is "SENT" in ASCII) but must never change, or two
// versions of the app could migrate concurrently.
const advisoryLockKey int64 = 0x53454E54

// Run applies, in lexical filename order, every *.sql file at the root of
// fsys that is not yet recorded in schema_migrations. The whole run is
// serialized via a Postgres advisory lock, and each migration plus its
// bookkeeping row is applied in a single transaction (Postgres DDL is
// transactional), so a failed migration leaves no half-applied schema.
func Run(ctx context.Context, db *sqlx.DB, fsys fs.FS) error {
	names, err := sqlFiles(fsys)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	// Session-level advisory locks must be released on the same connection
	// that acquired them, so pin a dedicated connection for the whole run
	// instead of letting the pool hand lock and unlock to different sessions.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Error("close migration connection", "error", err)
		}
	}()

	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer func() {
		// Best effort: closing the connection also releases session-level
		// advisory locks, so a failed unlock (e.g. cancelled ctx) is safe.
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey); err != nil {
			slog.Error("release advisory lock", "error", err)
		}
	}()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (filename TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, name := range names {
		var count int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE filename = $1`, name).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		if err := apply(ctx, conn, name, string(content)); err != nil {
			return err
		}
		slog.Info("applied migration", "file", name)
	}
	return nil
}

// apply runs one migration's SQL and its schema_migrations insert in a
// single transaction, begun on the pinned connection so everything happens
// in the session that holds the advisory lock.
func apply(ctx context.Context, conn *sql.Conn, name, content string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	if _, err := tx.ExecContext(ctx, content); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}

// sqlFiles returns the names of the *.sql files at the root of fsys in
// lexical order; migration filenames are zero-padded, so lexical order is
// apply order.
func sqlFiles(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

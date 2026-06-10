//go:build integration

package migrate_test

import (
	"context"
	"os"
	"testing"
	"testing/fstest"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/williamokano/sentinelsnap/internal/migrate"
)

func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; skipping integration tests")
	}
	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRun_AppliesOnceAndRollsBackFailures(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	t.Cleanup(func() {
		db.MustExec(`DROP TABLE IF EXISTS migrate_test_ok`)
		db.MustExec(`DELETE FROM schema_migrations WHERE filename LIKE 'zzz_migrate_test_%'`)
	})

	ok := fstest.MapFS{
		"zzz_migrate_test_001.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE migrate_test_ok (id INT)`)},
	}
	require.NoError(t, migrate.Run(ctx, db, ok))
	// Second run must be a no-op: the migration is already recorded.
	require.NoError(t, migrate.Run(ctx, db, ok))

	var n int
	require.NoError(t, db.Get(&n, `SELECT COUNT(*) FROM schema_migrations WHERE filename = 'zzz_migrate_test_001.sql'`))
	assert.Equal(t, 1, n)

	// A failing migration must roll back both its DDL and its bookkeeping row.
	bad := fstest.MapFS{
		"zzz_migrate_test_002.sql": &fstest.MapFile{Data: []byte(
			`CREATE TABLE migrate_test_bad (id INT); SELECT no_such_function()`,
		)},
	}
	require.Error(t, migrate.Run(ctx, db, bad))

	require.NoError(t, db.Get(&n, `SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'migrate_test_bad'`))
	assert.Zero(t, n)
	require.NoError(t, db.Get(&n, `SELECT COUNT(*) FROM schema_migrations WHERE filename = 'zzz_migrate_test_002.sql'`))
	assert.Zero(t, n)
}

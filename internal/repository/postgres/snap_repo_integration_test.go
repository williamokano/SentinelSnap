//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/williamokano/sentinelsnap/internal/domain"
	"github.com/williamokano/sentinelsnap/internal/repository/postgres"
)

func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set; skipping integration tests")
	}
	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	return db
}

func TestCreateSnap_and_ListSnaps(t *testing.T) {
	db := openTestDB(t)
	tx := db.MustBegin()
	t.Cleanup(func() { tx.Rollback() })

	repo := postgres.New(tx.Unsafe())

	ctx := context.Background()
	snapID, err := repo.CreateSnap(ctx, &domain.Snap{Latitude: 37.77, Longitude: -122.41})
	require.NoError(t, err)
	assert.Positive(t, snapID)

	photoID, err := repo.AddPhoto(ctx, &domain.Photo{
		SnapID:    snapID,
		URL:       "http://example.com/photo.jpg",
		StoredKey: "snaps/1/photo.jpg",
	})
	require.NoError(t, err)
	assert.Positive(t, photoID)

	snaps, err := repo.ListSnaps(ctx)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, snapID, snaps[0].ID)
	require.Len(t, snaps[0].Photos, 1)
	assert.Equal(t, photoID, snaps[0].Photos[0].ID)
}

func TestGetSnapByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.New(db)

	_, err := repo.GetSnapByID(context.Background(), 999999999)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

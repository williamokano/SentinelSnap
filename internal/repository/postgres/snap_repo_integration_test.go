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
	"github.com/williamokano/sentinelsnap/internal/repository"
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
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCreateSnapWithPhotos_and_ListSnaps(t *testing.T) {
	db := openTestDB(t)
	var repo repository.SnapRepository = postgres.New(db)

	ctx := context.Background()
	snap := &domain.Snap{Latitude: 37.77, Longitude: -122.41}
	photos := []domain.Photo{
		{Token: "tok-integration-1", StoredKey: "snaps/1/photo1.jpg"},
		{Token: "tok-integration-2", StoredKey: "snaps/1/photo2.jpg"},
	}
	require.NoError(t, repo.CreateSnapWithPhotos(ctx, snap, photos))
	// CreateSnapWithPhotos opens its own transaction, so clean up via the
	// repository (the cascade removes the photo rows).
	t.Cleanup(func() { _ = repo.DeleteSnap(context.Background(), snap.ID) })

	assert.Positive(t, snap.ID)
	for _, p := range photos {
		assert.Positive(t, p.ID)
		assert.Equal(t, snap.ID, p.SnapID)
	}

	got, err := repo.GetSnapByID(ctx, snap.ID)
	require.NoError(t, err)
	require.Len(t, got.Photos, 2)
	assert.Equal(t, photos[0].ID, got.Photos[0].ID)
	assert.Equal(t, photos[1].ID, got.Photos[1].ID)
}

func TestGetSnapByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.New(db)

	_, err := repo.GetSnapByID(context.Background(), 999999999)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

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
		require.NotNil(t, p.SnapID)
		assert.Equal(t, snap.ID, *p.SnapID)
	}

	got, err := repo.GetSnapByID(ctx, snap.ID)
	require.NoError(t, err)
	require.Len(t, got.Photos, 2)
	assert.Equal(t, photos[0].ID, got.Photos[0].ID)
	assert.Equal(t, photos[1].ID, got.Photos[1].ID)
}

func TestStandalonePhotos_CreateListDeleteAndViews(t *testing.T) {
	db := openTestDB(t)
	var repo repository.SnapRepository = postgres.New(db)
	ctx := context.Background()

	photos := []domain.Photo{
		{Token: "tok-standalone-1", StoredKey: "photos/standalone1.jpg"},
		{Token: "tok-standalone-2", StoredKey: "photos/standalone2.jpg"},
	}
	require.NoError(t, repo.CreatePhotos(ctx, photos))
	for _, p := range photos {
		assert.Positive(t, p.ID)
		assert.Nil(t, p.SnapID, "standalone uploads have no snap")
	}
	t.Cleanup(func() {
		for _, p := range photos {
			_ = repo.DeletePhoto(context.Background(), p.ID)
		}
	})

	all, err := repo.ListAllPhotos(ctx)
	require.NoError(t, err)
	ids := map[int64]bool{}
	for _, p := range all {
		ids[p.ID] = true
	}
	assert.True(t, ids[photos[0].ID] && ids[photos[1].ID], "both standalone photos are listed")

	// View count starts at 0 and increments on each open.
	got, err := repo.GetPhotoByToken(ctx, "tok-standalone-1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.ViewCount)
	require.NoError(t, repo.IncrementPhotoViews(ctx, "tok-standalone-1"))
	require.NoError(t, repo.IncrementPhotoViews(ctx, "tok-standalone-1"))
	got, err = repo.GetPhotoByToken(ctx, "tok-standalone-1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.ViewCount)

	require.NoError(t, repo.DeletePhoto(ctx, photos[0].ID))
	_, err = repo.GetPhotoByToken(ctx, "tok-standalone-1")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestCountPhotosForSnap(t *testing.T) {
	db := openTestDB(t)
	var repo repository.SnapRepository = postgres.New(db)
	ctx := context.Background()

	snap := &domain.Snap{Latitude: 1, Longitude: 1}
	photos := []domain.Photo{
		{Token: "tok-count-1", StoredKey: "photos/count1.jpg"},
		{Token: "tok-count-2", StoredKey: "photos/count2.jpg"},
	}
	require.NoError(t, repo.CreateSnapWithPhotos(ctx, snap, photos))
	t.Cleanup(func() { _ = repo.DeleteSnap(context.Background(), snap.ID) })

	n, err := repo.CountPhotosForSnap(ctx, snap.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	require.NoError(t, repo.DeletePhoto(ctx, photos[0].ID))
	n, err = repo.CountPhotosForSnap(ctx, snap.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestGetSnapByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := postgres.New(db)

	_, err := repo.GetSnapByID(context.Background(), 999999999)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

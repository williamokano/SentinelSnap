package repository

import (
	"context"

	"github.com/williamokano/sentinelsnap/internal/domain"
)

type SnapRepository interface {
	// CreateSnapWithPhotos inserts the snap row and all photo rows in a
	// single transaction, populating the generated IDs (and each photo's
	// SnapID) back onto the passed structs.
	CreateSnapWithPhotos(ctx context.Context, snap *domain.Snap, photos []domain.Photo) error
	DeleteSnap(ctx context.Context, id int64) error
	UpdateSnapName(ctx context.Context, id int64, name string) error
	ListSnaps(ctx context.Context) ([]domain.Snap, error)
	GetSnapByID(ctx context.Context, id int64) (*domain.Snap, error)
	GetPhotoByToken(ctx context.Context, token string) (*domain.Photo, error)
	ListPhotosForSnap(ctx context.Context, snapID int64) ([]domain.Photo, error)

	// CreatePhotos inserts standalone photo rows (snap_id NULL), populating
	// the generated IDs back onto the passed structs.
	CreatePhotos(ctx context.Context, photos []domain.Photo) error
	// ListAllPhotos returns every photo — snap-linked and standalone —
	// newest first.
	ListAllPhotos(ctx context.Context) ([]domain.Photo, error)
	DeletePhoto(ctx context.Context, id int64) error
	// CountPhotosForSnap reports how many photos still reference a snap, used
	// to detect when a snap's last photo has been deleted.
	CountPhotosForSnap(ctx context.Context, snapID int64) (int, error)
	IncrementPhotoViews(ctx context.Context, token string) error
}

package repository

import (
	"context"

	"github.com/williamokano/sentinelsnap/internal/domain"
)

type SnapRepository interface {
	CreateSnap(ctx context.Context, snap *domain.Snap) (int64, error)
	AddPhoto(ctx context.Context, photo *domain.Photo) (int64, error)
	DeleteSnap(ctx context.Context, id int64) error
	ListSnaps(ctx context.Context) ([]domain.Snap, error)
	GetSnapByID(ctx context.Context, id int64) (*domain.Snap, error)
	GetPhotoByToken(ctx context.Context, token string) (*domain.Photo, error)
	ListPhotosForSnap(ctx context.Context, snapID int64) ([]domain.Photo, error)
}

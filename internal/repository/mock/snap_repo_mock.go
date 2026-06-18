package mock

import (
	"context"

	"github.com/stretchr/testify/mock"
	"github.com/williamokano/sentinelsnap/internal/domain"
)

type SnapRepository struct {
	mock.Mock
}

func (m *SnapRepository) CreateSnapWithPhotos(ctx context.Context, snap *domain.Snap, photos []domain.Photo) error {
	return m.Called(ctx, snap, photos).Error(0)
}

func (m *SnapRepository) DeleteSnap(ctx context.Context, id int64) error {
	return m.Called(ctx, id).Error(0)
}

func (m *SnapRepository) UpdateSnapName(ctx context.Context, id int64, name string) error {
	return m.Called(ctx, id, name).Error(0)
}

func (m *SnapRepository) ListSnaps(ctx context.Context) ([]domain.Snap, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.Snap), args.Error(1)
}

func (m *SnapRepository) GetSnapByID(ctx context.Context, id int64) (*domain.Snap, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Snap), args.Error(1)
}

func (m *SnapRepository) GetPhotoByToken(ctx context.Context, token string) (*domain.Photo, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Photo), args.Error(1)
}

func (m *SnapRepository) ListPhotosForSnap(ctx context.Context, snapID int64) ([]domain.Photo, error) {
	args := m.Called(ctx, snapID)
	return args.Get(0).([]domain.Photo), args.Error(1)
}

func (m *SnapRepository) CreatePhotos(ctx context.Context, photos []domain.Photo) error {
	return m.Called(ctx, photos).Error(0)
}

func (m *SnapRepository) ListAllPhotos(ctx context.Context) ([]domain.Photo, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.Photo), args.Error(1)
}

func (m *SnapRepository) DeletePhoto(ctx context.Context, id int64) error {
	return m.Called(ctx, id).Error(0)
}

func (m *SnapRepository) CountPhotosForSnap(ctx context.Context, snapID int64) (int, error) {
	args := m.Called(ctx, snapID)
	return args.Int(0), args.Error(1)
}

func (m *SnapRepository) IncrementPhotoViews(ctx context.Context, token string) error {
	return m.Called(ctx, token).Error(0)
}

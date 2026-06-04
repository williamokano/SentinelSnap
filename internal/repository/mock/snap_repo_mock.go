package mock

import (
	"context"

	"github.com/stretchr/testify/mock"
	"github.com/williamokano/sentinelsnap/internal/domain"
)

type SnapRepository struct {
	mock.Mock
}

func (m *SnapRepository) CreateSnap(ctx context.Context, snap *domain.Snap) (int64, error) {
	args := m.Called(ctx, snap)
	return args.Get(0).(int64), args.Error(1)
}

func (m *SnapRepository) AddPhoto(ctx context.Context, photo *domain.Photo) (int64, error) {
	args := m.Called(ctx, photo)
	return args.Get(0).(int64), args.Error(1)
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

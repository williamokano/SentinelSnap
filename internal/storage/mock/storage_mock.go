package mock

import (
	"context"
	"io"

	"github.com/stretchr/testify/mock"
)

type StorageProvider struct {
	mock.Mock
}

func (m *StorageProvider) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	return m.Called(ctx, key, r, contentType).Error(0)
}

func (m *StorageProvider) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.String(1), args.Error(2)
	}
	return args.Get(0).(io.ReadCloser), args.String(1), args.Error(2)
}

func (m *StorageProvider) Delete(ctx context.Context, key string) error {
	return m.Called(ctx, key).Error(0)
}

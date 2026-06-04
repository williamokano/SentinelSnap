package mock

import (
	"context"
	"io"

	"github.com/stretchr/testify/mock"
)

type StorageProvider struct {
	mock.Mock
}

func (m *StorageProvider) Put(ctx context.Context, key string, r io.Reader, contentType string) (string, error) {
	args := m.Called(ctx, key, r, contentType)
	return args.String(0), args.Error(1)
}

func (m *StorageProvider) Delete(ctx context.Context, key string) error {
	return m.Called(ctx, key).Error(0)
}

func (m *StorageProvider) URL(key string) string {
	return m.Called(key).String(0)
}

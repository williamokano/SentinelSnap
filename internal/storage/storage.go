package storage

import (
	"context"
	"io"
)

type StorageProvider interface {
	Put(ctx context.Context, key string, r io.Reader, contentType string) (url string, err error)
	Delete(ctx context.Context, key string) error
	URL(key string) string
}

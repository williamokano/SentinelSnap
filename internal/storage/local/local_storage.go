package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	baseDir string
	baseURL string
}

func New(baseDir, baseURL string) (*LocalStorage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &LocalStorage{
		baseDir: baseDir,
		baseURL: strings.TrimRight(baseURL, "/"),
	}, nil
}

func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	dest := filepath.Join(s.baseDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", fmt.Errorf("create dirs for key %q: %w", key, err)
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("create file %q: %w", dest, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("write file %q: %w", dest, err)
	}
	return s.URL(key), nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	return os.Remove(filepath.Join(s.baseDir, filepath.FromSlash(key)))
}

func (s *LocalStorage) URL(key string) string {
	return s.baseURL + "/" + key
}

package local

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
)

type LocalStorage struct {
	baseDir string
}

func New(baseDir string) (*LocalStorage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &LocalStorage{baseDir: baseDir}, nil
}

func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ string) error {
	dest := filepath.Join(s.baseDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create dirs for key %q: %w", key, err)
	}
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create file %q: %w", dest, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("write file %q: %w", dest, err)
	}
	return nil
}

func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, string, error) {
	dest := filepath.Join(s.baseDir, filepath.FromSlash(key))
	f, err := os.Open(dest)
	if err != nil {
		return nil, "", fmt.Errorf("open file %q: %w", dest, err)
	}
	ct := mime.TypeByExtension(filepath.Ext(dest))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return f, ct, nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	dest := filepath.Join(s.baseDir, filepath.FromSlash(key))
	if err := os.Remove(dest); err != nil {
		return err
	}
	_ = os.Remove(filepath.Dir(dest))
	return nil
}

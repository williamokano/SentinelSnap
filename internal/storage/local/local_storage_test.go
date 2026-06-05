package local_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/williamokano/sentinelsnap/internal/storage/local"
)

func TestPut_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	s, err := local.New(dir)
	require.NoError(t, err)

	err = s.Put(context.Background(), "test.jpg", strings.NewReader("imgdata"), "image/jpeg")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "test.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "imgdata", string(data))
}

func TestPut_CreatesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	s, err := local.New(dir)
	require.NoError(t, err)

	err = s.Put(context.Background(), "snaps/7/photo.jpg", strings.NewReader("data"), "image/jpeg")
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "snaps", "7", "photo.jpg"))
	require.NoError(t, err)
}

func TestGet_ReturnsContent(t *testing.T) {
	dir := t.TempDir()
	s, err := local.New(dir)
	require.NoError(t, err)

	err = s.Put(context.Background(), "snap.jpg", strings.NewReader("imgdata"), "image/jpeg")
	require.NoError(t, err)

	rc, ct, err := s.Get(context.Background(), "snap.jpg")
	require.NoError(t, err)
	defer rc.Close()

	assert.Equal(t, "image/jpeg", ct)
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "imgdata", string(data))
}

func TestDelete_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	s, err := local.New(dir)
	require.NoError(t, err)

	err = s.Put(context.Background(), "snap.jpg", strings.NewReader("x"), "image/jpeg")
	require.NoError(t, err)

	require.NoError(t, s.Delete(context.Background(), "snap.jpg"))

	_, err = os.Stat(filepath.Join(dir, "snap.jpg"))
	assert.True(t, os.IsNotExist(err))
}

func TestGet_MissingFileReturnsError(t *testing.T) {
	s, err := local.New(t.TempDir())
	require.NoError(t, err)

	rc, _, err := s.Get(context.Background(), "nonexistent.jpg")
	assert.Error(t, err)
	assert.Nil(t, rc)
}

func TestDelete_MissingFileReturnsError(t *testing.T) {
	s, err := local.New(t.TempDir())
	require.NoError(t, err)

	err = s.Delete(context.Background(), "ghost.jpg")
	assert.Error(t, err)
}

func TestDelete_NestedDirCleanup(t *testing.T) {
	dir := t.TempDir()
	s, err := local.New(dir)
	require.NoError(t, err)

	// Put a file in a nested directory.
	require.NoError(t, s.Put(context.Background(), "photos/token123.jpg", strings.NewReader("data"), "image/jpeg"))

	nestedDir := filepath.Join(dir, "photos")
	_, statErr := os.Stat(nestedDir)
	require.NoError(t, statErr, "nested directory should exist after Put")

	// Delete the file — the implementation also attempts to remove the parent dir.
	require.NoError(t, s.Delete(context.Background(), "photos/token123.jpg"))

	_, err = os.Stat(filepath.Join(dir, "photos", "token123.jpg"))
	assert.True(t, os.IsNotExist(err), "file should be removed")

	// The parent directory should also have been cleaned up (best-effort).
	_, err = os.Stat(nestedDir)
	assert.True(t, os.IsNotExist(err), "empty parent directory should be removed after delete")
}

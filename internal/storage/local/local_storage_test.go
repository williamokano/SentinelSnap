package local_test

import (
	"context"
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

	url, err := s.Put(context.Background(), "test.jpg", strings.NewReader("imgdata"), "image/jpeg")
	require.NoError(t, err)
	assert.Equal(t, "/uploads/test.jpg", url)

	data, err := os.ReadFile(filepath.Join(dir, "test.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "imgdata", string(data))
}

func TestPut_CreatesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	s, err := local.New(dir)
	require.NoError(t, err)

	_, err = s.Put(context.Background(), "snaps/7/photo.jpg", strings.NewReader("data"), "image/jpeg")
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "snaps", "7", "photo.jpg"))
	require.NoError(t, err)
}

func TestDelete_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	s, err := local.New(dir)
	require.NoError(t, err)

	_, err = s.Put(context.Background(), "snap.jpg", strings.NewReader("x"), "image/jpeg")
	require.NoError(t, err)

	require.NoError(t, s.Delete(context.Background(), "snap.jpg"))

	_, err = os.Stat(filepath.Join(dir, "snap.jpg"))
	assert.True(t, os.IsNotExist(err))
}

func TestURL_ReturnsRelativePath(t *testing.T) {
	s, _ := local.New(t.TempDir())
	assert.Equal(t, "/uploads/snaps/1/img.jpg", s.URL("snaps/1/img.jpg"))
}

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

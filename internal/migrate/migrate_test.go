package migrate

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLFiles_SortsAndFilters(t *testing.T) {
	fsys := fstest.MapFS{
		"002_create_photos.sql": &fstest.MapFile{Data: []byte("two")},
		"010_later.sql":         &fstest.MapFile{Data: []byte("ten")},
		"001_create_snaps.sql":  &fstest.MapFile{Data: []byte("one")},
		"embed.go":              &fstest.MapFile{Data: []byte("package migrations")},
		"notes.txt":             &fstest.MapFile{Data: []byte("not sql")},
		"sub/099_nested.sql":    &fstest.MapFile{Data: []byte("nested, ignored")},
	}

	names, err := sqlFiles(fsys)
	require.NoError(t, err)
	assert.Equal(t, []string{"001_create_snaps.sql", "002_create_photos.sql", "010_later.sql"}, names)
}

func TestSQLFiles_Empty(t *testing.T) {
	names, err := sqlFiles(fstest.MapFS{})
	require.NoError(t, err)
	assert.Empty(t, names)
}

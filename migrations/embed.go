// Package migrations holds the canonical SQL migration files, embedded so
// the binary can apply them at startup (see internal/migrate).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

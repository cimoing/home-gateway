package migrations

import "embed"

// Files contains SQLite migrations.
//
//go:embed sqlite/*.sql
var Files embed.FS

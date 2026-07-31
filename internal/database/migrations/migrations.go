package migrations

import "embed"

// Files contains migrations for every supported SQL dialect.
//
//go:embed sqlite/*.sql postgres/*.sql mysql/*.sql
var Files embed.FS

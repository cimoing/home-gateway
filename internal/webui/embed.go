package webui

import (
	"embed"
	"io/fs"
)

// Dist contains the built Vue assets under dist/.
//
//go:embed all:dist
var dist embed.FS

// FS returns the embedded web root (contents of dist/).
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return dist
	}
	return sub
}

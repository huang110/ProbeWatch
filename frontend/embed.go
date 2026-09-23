package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns an fs.FS sub-filesystem rooted at the dist directory.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

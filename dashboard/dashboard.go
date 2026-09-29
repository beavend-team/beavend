package dashboard

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the sub-filesystem rooted inside the compiled dist directory.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

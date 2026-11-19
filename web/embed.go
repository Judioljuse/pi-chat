// Package web embeds the built browser client (web-src builds into dist/).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS is the contents of dist/: index.html at the root, hashed bundles under assets/.
var FS, _ = fs.Sub(dist, "dist")

// Package web embeds the browser client.
package web

import "embed"

//go:embed *.html *.js *.css *.svg *.webmanifest vendor
var FS embed.FS

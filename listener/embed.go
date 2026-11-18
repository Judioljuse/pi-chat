// Package listener embeds the pichat-listen.mjs script for download.
package listener

import _ "embed"

//go:embed pichat-listen.mjs
var Script []byte

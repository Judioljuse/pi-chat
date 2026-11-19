// Package version holds the pichat release version.
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var embedded string

// override replaces the embedded version in test builds:
// -ldflags "-X pichat/internal/version.override=0.1.0".
var override string

// Version is the running build's version without a leading 'v'.
var Version = func() string {
	if override != "" {
		return override
	}
	return strings.TrimSpace(embedded)
}()

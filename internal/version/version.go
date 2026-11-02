// Package version holds build information set with -ldflags.
package version

// Set at build time; git tags are the version.
var (
	Version = "dev"
	Commit  = "none"
)

// Package version holds the build-time version string, so both pkg/app
// (--version, config show) and pkg/gui (help popup) can read it without
// depending on each other.
package version

// Version is overridden at build time via
// -ldflags "-X github.com/thomas-gleizes/lazyshell/pkg/version.Version=...".
// "dev" is what `go run`/`go build` without ldflags produce, so it never
// looks like a real release.
var Version = "dev"

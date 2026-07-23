// Package buildinfo holds build-time metadata injected via -ldflags.
package buildinfo

import "fmt"

// Version, Commit, and BuildTime are populated at build time via -ldflags
// (see Makefile / .goreleaser.yml). They default to "dev"/"unknown" for
// local `go build`/`go run` invocations that skip ldflags injection.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// String renders the build metadata as a single human-readable line.
func String() string {
	return fmt.Sprintf("nexus3-go %s (commit %s, built %s)", Version, Commit, BuildTime)
}

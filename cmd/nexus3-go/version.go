package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

// Build metadata. Release tooling injects these with
// -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// buildVersion returns the version to report. A value injected through
// -ldflags wins; otherwise the main module version recorded by the Go
// toolchain is used (so "go install ...@v1.2.3" reports v1.2.3), and "dev"
// is the last resort.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}

	return version
}

// versionString renders the one-line version banner.
func versionString() string {
	return fmt.Sprintf("nexus3-go %s (commit %s, built %s)", buildVersion(), commit, date)
}

// printVersion writes the version banner to w.
func printVersion(w io.Writer) {
	_, _ = fmt.Fprintln(w, versionString())
}

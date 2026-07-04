// Package version holds build metadata stamped at link time. All binaries share
// this single ldflags path:
//
//	-X github.com/NerdMeNot/flint/internal/version.Version=v0.1.0
//	-X github.com/NerdMeNot/flint/internal/version.Commit=abc1234
package version

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = ""
)

// String renders the human-readable version line used by --version flags.
func String() string {
	return fmt.Sprintf("%s (%s)", Version, Commit)
}

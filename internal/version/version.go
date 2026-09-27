// Package version holds build identity. Version and Commit are set at build
// time with -ldflags "-X github.com/dynamatt/provenance/internal/version.Version=..."
// rather than read from the environment or the VCS at runtime, so the same
// source and flags always produce the same binary.
package version

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

// String is the multi-line form printed by `provenance version` and
// `provenance --version`.
func String() string {
	return fmt.Sprintf("provenance %s\ncommit: %s\ngo: %s\n", Version, Commit, runtime.Version())
}

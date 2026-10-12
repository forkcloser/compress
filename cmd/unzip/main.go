// Command unzip lists, tests and extracts zip archives with Info-ZIP's
// grammar, for the subset of its options the rig's scripts use, in pure Go
// on every platform. See internal/unzipcmd for the exact surface.
package main

import (
	"os"

	"github.com/forkcloser/compress/internal/unzipcmd"
)

// version is set by the release build (-X main.version=…).
var version = "dev"

func main() {
	os.Exit(unzipcmd.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}

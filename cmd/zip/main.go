// Command zip creates and updates zip archives with Info-ZIP's grammar, for
// the subset of its options the rig's scripts and curl-for-win use, in pure
// Go on every platform. See internal/zipcmd for the exact surface.
package main

import (
	"os"

	"github.com/forkcloser/compress/internal/zipcmd"
)

// version is set by the release build (-X main.version=…).
var version = "dev"

func main() {
	os.Exit(zipcmd.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}

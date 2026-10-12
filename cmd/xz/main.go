// Command xz compresses and decompresses files in the .xz format with xz's
// grammar, for the options the rig uses, in pure Go on every platform. See
// internal/xzcmd for the exact surface.
package main

import (
	"os"

	"github.com/forkcloser/compress/internal/xzcmd"
)

// version is set by the release build (-X main.version=…).
var version = "dev"

func main() {
	os.Exit(xzcmd.Run(os.Args[0], os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}

// Command gzip compresses and decompresses files in the gzip format with
// gzip's grammar, for the options the rig uses, in pure Go on every
// platform. See internal/gzipcmd for the exact surface.
package main

import (
	"os"

	"github.com/forkcloser/compress/internal/gzipcmd"
)

// version is set by the release build (-X main.version=…).
var version = "dev"

func main() {
	os.Exit(gzipcmd.Run(os.Args[0], os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}

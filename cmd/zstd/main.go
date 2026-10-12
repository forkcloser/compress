// Command zstd compresses and decompresses files in the Zstandard format
// with zstd's grammar, for the common options, in pure Go on every platform.
// See internal/zstdcmd for the exact surface.
package main

import (
	"os"

	"github.com/forkcloser/compress/internal/zstdcmd"
)

// version is set by the release build (-X main.version=…).
var version = "dev"

func main() {
	os.Exit(zstdcmd.Run(os.Args[0], os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}

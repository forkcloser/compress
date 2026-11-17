//go:build unix

package filecodec

import (
	"os"
	"syscall"
)

// hardLinked is whether the file has names other than the one it was
// found under.
func hardLinked(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)

	return ok && stat.Nlink > 1
}

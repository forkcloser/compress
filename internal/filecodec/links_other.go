//go:build !unix

package filecodec

import "os"

// hardLinked is never true here: what Stat answers carries no link count,
// so a file with other names is read as any file.
func hardLinked(os.FileInfo) bool {
	return false
}

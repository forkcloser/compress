//go:build unix

package sparse

import "os"

// markSparse is nothing here: a write past a file's end leaves a hole.
func markSparse(*os.File) {}

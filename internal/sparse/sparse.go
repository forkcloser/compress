// Package sparse extracts a tar archive's sparse members: a file is written
// with its zeros left out, so that what the file never held takes no room on
// disk, as where it was archived from.
package sparse

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// block is the unit zeros are looked for in: a whole block of them is
// stepped over, and so is a shorter run that ends a write.
const block = 4096

//nolint:gochecknoglobals // a block of zeros to compare with, never written
var zeros [block]byte

// Member reports an entry stored as a map of its data and holes, in the old
// GNU form or in one of the PAX ones. archive/tar reads such an entry as
// its whole size, the holes as zeros, and gives no other sign of them.
func Member(header *tar.Header) bool {
	if header.Typeflag == tar.TypeGNUSparse {
		return true
	}

	for key := range header.PAXRecords {
		if strings.HasPrefix(key, "GNU.sparse.") {
			return true
		}
	}

	return false
}

// Writer writes to a file from where the file is, and steps over every
// whole block of zeros it is given instead of writing it.
type Writer struct {
	file *os.File
	// stepped is the zeros met since the last write and not stepped over
	// in the file yet.
	stepped int64
}

// NewWriter gives file, which is empty, its size, and returns the Writer of
// its content. The size is the file's from the start, so zeros stepped over at
// its end are part of it, and a filesystem that cannot hold a file that
// long says so here, before anything is written.
func NewWriter(file *os.File, size int64) (*Writer, error) {
	// A filesystem with no holes fills them itself: the file is whole
	// either way.
	markSparse(file)

	if err := file.Truncate(size); err != nil {
		return nil, fmt.Errorf("size: %w", err)
	}

	return &Writer{file: file}, nil
}

// Write writes p, less its blocks of zeros.
func (w *Writer) Write(p []byte) (int, error) {
	done := 0

	for done < len(p) {
		empty := run(p[done:], true)
		w.stepped += int64(empty)
		done += empty

		data := run(p[done:], false)
		if data == 0 {
			continue
		}

		if w.stepped > 0 {
			if _, err := w.file.Seek(w.stepped, io.SeekCurrent); err != nil {
				return done, fmt.Errorf("seek: %w", err)
			}

			w.stepped = 0
		}

		n, err := w.file.Write(p[done : done+data])
		done += n

		if err != nil {
			return done, err //nolint:wrapcheck // the file's own error
		}
	}

	return done, nil
}

// run is the length of the blocks at the head of p that are all zeros, or
// that are not, as empty asks. The last block of p may be a short one.
func run(p []byte, empty bool) int {
	n := 0

	for n < len(p) {
		end := min(n+block, len(p))
		if bytes.Equal(p[n:end], zeros[:end-n]) != empty {
			break
		}

		n = end
	}

	return n
}

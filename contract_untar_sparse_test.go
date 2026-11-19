package compress_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/tarblocks"
)

// TestContractUntarLeavesASparseFilesHolesAsHoles: "a sparse file is
// extracted with its holes left as holes: it takes on disk about what the
// archive carries of it", in each of the four forms a tar stores one in.
// The file is the 64 MiB it states and reads as its pieces among zeros, and
// the entry after it is extracted too.
func TestContractUntarLeavesASparseFilesHolesAsHoles(t *testing.T) {
	t.Parallel()

	// What the file may take on disk: its pieces are some 14 KiB.
	const held = 1 << 20

	realSize, pieces := tarblocks.Holed()

	for _, form := range tarblocks.SparseForms() {
		t.Run(form, func(t *testing.T) {
			t.Parallel()

			root, dir := openRoot(t)

			if err := compress.Untar(bytes.NewReader(tarblocks.Sparse(form, realSize, pieces...)), root); err != nil {
				t.Fatal(err)
			}

			hole := filepath.Join(dir, "hole")

			file, err := os.Open(hole)
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = file.Close() }()

			if wrong := tarblocks.Check(file, realSize, pieces...); wrong != nil {
				t.Error(wrong)
			}

			onDisk, err := tarblocks.OnDisk(hole)
			if err != nil {
				t.Fatal(err)
			}

			if onDisk > held {
				t.Errorf("hole takes %d bytes on disk, for some 14 KiB of data", onDisk)
			}

			if got, err := os.ReadFile(filepath.Join(dir, "after")); err != nil || string(got) != "after" {
				t.Errorf("the entry after it: %q, %v", got, err)
			}
		})
	}
}

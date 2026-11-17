package compress_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/constants"
	"github.com/forkcloser/compress/internal/tarblocks"
)

// TestContractUntarRefusesASparseFile: "an archive that holds one is
// refused, ErrCorrupt, where it is met", in each of the four forms: nothing
// is written at its name, though it states 64 MiB in a few blocks, and
// Untar does not go on to the entry after it.
func TestContractUntarRefusesASparseFile(t *testing.T) {
	t.Parallel()

	for _, form := range tarblocks.SparseForms() {
		t.Run(form, func(t *testing.T) {
			t.Parallel()

			root, dir := openRoot(t)

			err := compress.Untar(bytes.NewReader(tarblocks.Sparse(form, 64*constants.MiB)), root)
			if !errors.Is(err, compress.ErrCorrupt) {
				t.Errorf("Untar: %v; want ErrCorrupt", err)
			}

			for _, name := range []string{"hole", "after"} {
				if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("%s was written: %v", name, err)
				}
			}
		})
	}
}

// TestContractUntarExtractsAContiguousFile: a contiguous file, type '7', is
// a regular file to every tar and to Untar: it is extracted, not skipped as
// the kinds Untar does not write are.
func TestContractUntarExtractsAContiguousFile(t *testing.T) {
	t.Parallel()

	archive := archiveOf(t, func(w *tar.Writer) {
		writeEntry(t, w, &tar.Header{Name: "contiguous", Typeflag: tar.TypeCont, Mode: 0o644}, "0123456789")
	})

	root, dir := openRoot(t)

	if err := compress.Untar(bytes.NewReader(archive), root); err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(filepath.Join(dir, "contiguous")); err != nil || string(got) != "0123456789" {
		t.Errorf("contiguous: %q, %v", got, err)
	}
}

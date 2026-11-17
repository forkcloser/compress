package tarcmd_test

import (
	"archive/tar"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/tarblocks"
	"github.com/forkcloser/compress/internal/tarcmd"
)

// TestContractSparseMemberIsRefused: a sparse member states a size and
// carries a map of it; read as a file it is that size, the holes as zeros.
// Extraction refuses it, in the three PAX forms and in the old GNU one, writes
// nothing at its name, extracts the member after it and exits with failure:
// here two archives of a few blocks each state 64 MiB with no data at all.
func TestContractSparseMemberIsRefused(t *testing.T) {
	t.Parallel()

	const realSize = 64 << 20

	for _, name := range tarblocks.SparseForms() {
		archive := tarblocks.Sparse(name, realSize)

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			listed, _ := mustRun(t, archive, "-tf", "-")
			if listed != "hole\nafter\n" {
				t.Fatalf("the archive lists %q", listed)
			}

			dst := t.TempDir()

			status, _, stderr := run(t, archive, "-xf", "-", "-C", dst)
			if status != tarcmd.ExitError ||
				!strings.Contains(stderr, "tar: hole: Cannot extract: sparse member is not supported") {
				t.Errorf("exit %d, stderr %q", status, stderr)
			}

			if _, err := os.Lstat(filepath.Join(dst, "hole")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("hole was written: %v", err)
			}

			if got := readFile(t, filepath.Join(dst, "after")); got != "after" {
				t.Errorf("the member after it: %q", got)
			}
		})
	}
}

// TestContractDeviceIsRefused: a character and a block device are reported
// and skipped, and the member after them is extracted.
func TestContractDeviceIsRefused(t *testing.T) {
	t.Parallel()

	archive := crafted(t,
		&tar.Header{Name: "char", Typeflag: tar.TypeChar, Mode: 0o644, Devmajor: 1, Devminor: 3},
		&tar.Header{Name: "block", Typeflag: tar.TypeBlock, Mode: 0o644, Devmajor: 8},
		&tar.Header{Name: "after", Typeflag: tar.TypeReg, Mode: 0o644},
	)
	dst := t.TempDir()

	status, _, stderr := run(t, archive, "-xf", "-", "-C", dst)
	if status != tarcmd.ExitError || !strings.Contains(stderr, "tar: char: Cannot mknod") ||
		!strings.Contains(stderr, "tar: block: Cannot mknod") {
		t.Errorf("exit %d, stderr %q", status, stderr)
	}

	for _, name := range []string{"char", "block"} {
		if _, err := os.Lstat(filepath.Join(dst, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s was written: %v", name, err)
		}
	}

	if _, err := os.Lstat(filepath.Join(dst, "after")); err != nil {
		t.Errorf("the member after them: %v", err)
	}
}

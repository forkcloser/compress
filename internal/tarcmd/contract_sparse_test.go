package tarcmd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/reference"
	"github.com/forkcloser/compress/internal/tarblocks"
	"github.com/forkcloser/compress/internal/tarcmd"
)

// heldBySparse is what a sparse member of these tests may take on disk
// once extracted: its pieces are some 14 KiB, the size it states 64 MiB.
const heldBySparse = 1 << 20

// checkSparse reports a file that is not realSize bytes of the pieces among
// zeros, or that takes more room than its pieces need.
func checkSparse(t *testing.T, path string, realSize int64, pieces []tarblocks.Piece) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = file.Close() }()

	if wrong := tarblocks.Check(file, realSize, pieces...); wrong != nil {
		t.Errorf("%s: %v", path, wrong)
	}

	onDisk, err := tarblocks.OnDisk(path)
	if err != nil {
		t.Fatal(err)
	}

	if onDisk > heldBySparse {
		t.Errorf("%s takes %d bytes on disk, for some 14 KiB of data", path, onDisk)
	}
}

// TestContractSparseMemberKeepsItsHoles: "a sparse member is extracted with
// its holes left as holes", in the three PAX forms and in the old GNU one.
// The member is listed, extracted at the 64 MiB it states with its pieces
// among zeros, takes the room of its pieces, and the member after it is
// extracted.
func TestContractSparseMemberKeepsItsHoles(t *testing.T) {
	t.Parallel()

	realSize, pieces := tarblocks.Holed()

	for _, form := range tarblocks.SparseForms() {
		archive := tarblocks.Sparse(form, realSize, pieces...)

		t.Run(form, func(t *testing.T) {
			t.Parallel()

			listed, _ := mustRun(t, archive, "-tf", "-")
			if listed != "hole\nafter\n" {
				t.Fatalf("the archive lists %q", listed)
			}

			dst := t.TempDir()

			if _, stderr := mustRun(t, archive, "-xf", "-", "-C", dst); stderr != "" {
				t.Errorf("stderr %q", stderr)
			}

			checkSparse(t, filepath.Join(dst, "hole"), realSize, pieces)

			if got := readFile(t, filepath.Join(dst, "after")); got != "after" {
				t.Errorf("the member after it: %q", got)
			}
		})
	}
}

// TestContractSparseMemberCutShortIsNotLeftWhole: an archive that ends
// inside a sparse member is an error, and the file left is as long as what
// was written of it, not the size the member states.
func TestContractSparseMemberCutShortIsNotLeftWhole(t *testing.T) {
	t.Parallel()

	realSize, pieces := tarblocks.Holed()

	for _, form := range tarblocks.SparseForms() {
		// Half of the archive: its headers and a part of the first piece.
		whole := tarblocks.Sparse(form, realSize, pieces...)
		archive := whole[:len(whole)/2]

		t.Run(form, func(t *testing.T) {
			t.Parallel()

			dst := t.TempDir()

			status, _, stderr := run(t, archive, "-xf", "-", "-C", dst)
			if status != tarcmd.ExitError || stderr == "" {
				t.Errorf("exit %d, stderr %q", status, stderr)
			}

			info, err := os.Stat(filepath.Join(dst, "hole"))
			if err != nil {
				t.Fatal(err)
			}

			if info.Size() > int64(len(pieces[0].Data)) {
				t.Errorf("hole is left %d bytes long, of which the archive holds under %d",
					info.Size(), len(pieces[0].Data))
			}
		})
	}
}

// TestContractInteropSparseGNUTar: what GNU tar archives of a file with
// holes, in each form it writes, is extracted as GNU tar extracts it: the
// same bytes, and no more room on disk.
func TestContractInteropSparseGNUTar(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the tar on a Windows runner's PATH is Git for Windows' tar.exe, which fails on these paths " +
			"(exit 128): not a tar this package exchanges archives with")
	}

	system := reference.Find(t, "tar", "--version")

	version, err := exec.Command(system, "--version").Output()
	if err != nil || !strings.Contains(string(version), "GNU tar") {
		t.Skip("the tar on PATH is not GNU tar")
	}

	realSize, pieces := tarblocks.Holed()

	// The file GNU tar archives: the pieces written where they are, the
	// rest never written.
	src := t.TempDir()

	file, err := os.Create(filepath.Join(src, "hole"))
	if err != nil {
		t.Fatal(err)
	}

	for _, piece := range pieces {
		if _, err := file.WriteAt(piece.Data, piece.Offset); err != nil {
			t.Fatal(err)
		}
	}

	if err := file.Truncate(realSize); err != nil {
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	forms := map[string][]string{
		"old GNU": {"--format=gnu"},
		"PAX 0.0": {"--format=posix", "--sparse-version=0.0"},
		"PAX 0.1": {"--format=posix", "--sparse-version=0.1"},
		"PAX 1.0": {"--format=posix", "--sparse-version=1.0"},
	}

	for form, flags := range forms {
		t.Run(form, func(t *testing.T) {
			t.Parallel()

			archive := filepath.Join(t.TempDir(), "sparse.tar")

			args := append([]string{"--sparse", "-cf", archive, "-C", src}, flags...)
			if out, err := exec.Command(system, append(args, "hole")...).CombinedOutput(); err != nil {
				t.Fatalf("GNU tar: %v\n%s", err, out)
			}

			// GNU tar found the holes, or the archive proves nothing.
			if info, err := os.Stat(archive); err != nil || info.Size() > heldBySparse {
				t.Fatalf("GNU tar's archive: %v, %v", info, err)
			}

			theirs := t.TempDir()
			if out, err := exec.Command(system, "-xf", archive, "-C", theirs).CombinedOutput(); err != nil {
				t.Fatalf("GNU tar: %v\n%s", err, out)
			}

			mine := t.TempDir()
			mustRun(t, nil, "-xf", archive, "-C", mine)

			checkSparse(t, filepath.Join(mine, "hole"), realSize, pieces)

			onDisk, err := tarblocks.OnDisk(filepath.Join(mine, "hole"))
			if err != nil {
				t.Fatal(err)
			}

			theirsOnDisk, err := tarblocks.OnDisk(filepath.Join(theirs, "hole"))
			if err != nil {
				t.Fatal(err)
			}

			if onDisk > theirsOnDisk {
				t.Errorf("hole takes %d bytes on disk, GNU tar's %d", onDisk, theirsOnDisk)
			}
		})
	}
}

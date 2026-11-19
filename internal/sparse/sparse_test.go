package sparse_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/forkcloser/compress/internal/sparse"
	"github.com/forkcloser/compress/internal/tarblocks"
)

// held is what a file of the tests may take on disk and still be one whose
// holes were left out: the pieces are some 14 KiB, the size stated 64 MiB.
const held = 1 << 20

func create(t *testing.T) *os.File {
	t.Helper()

	file, err := os.Create(filepath.Join(t.TempDir(), "file"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = file.Close() })

	return file
}

// TestContractMemberIsASparseEntryInAnyForm: "an entry stored as a map of
// its data and holes, in the old GNU form or in one of the PAX ones", and
// no other.
func TestContractMemberIsASparseEntryInAnyForm(t *testing.T) {
	t.Parallel()

	realSize, pieces := tarblocks.Holed()

	for _, form := range tarblocks.SparseForms() {
		reader := tar.NewReader(bytes.NewReader(tarblocks.Sparse(form, realSize, pieces...)))

		header, err := reader.Next()
		if err != nil {
			t.Fatalf("%s: %v", form, err)
		}

		if header.Name != "hole" || header.Size != realSize || !sparse.Member(header) {
			t.Errorf("%s: %q of %d bytes, a sparse member: %v", form, header.Name, header.Size, sparse.Member(header))
		}

		if wrong := tarblocks.Check(reader, realSize, pieces...); wrong != nil {
			t.Errorf("%s reads as: %v", form, wrong)
		}

		header, err = reader.Next()
		if err != nil {
			t.Fatalf("%s: %v", form, err)
		}

		if header.Name != "after" || sparse.Member(header) {
			t.Errorf("%s: %q, a sparse member: %v", form, header.Name, sparse.Member(header))
		}
	}
}

// TestContractWriterLeavesBlocksOfZerosOut: the file is the size stated and
// reads as what was written, zeros where zeros were, and what was not
// written takes no room.
func TestContractWriterLeavesBlocksOfZerosOut(t *testing.T) {
	t.Parallel()

	realSize, pieces := tarblocks.Holed()
	archive := tarblocks.Sparse("PAX 1.0", realSize, pieces...)

	reader := tar.NewReader(bytes.NewReader(archive))
	if _, err := reader.Next(); err != nil {
		t.Fatal(err)
	}

	file := create(t)

	writer, err := sparse.NewWriter(file, realSize)
	if err != nil {
		t.Fatal(err)
	}

	copied, err := io.Copy(writer, reader)
	if err != nil || copied != realSize {
		t.Fatalf("copied %d bytes: %v", copied, err)
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}

	if err = tarblocks.Check(file, realSize, pieces...); err != nil {
		t.Error(err)
	}

	onDisk, err := tarblocks.OnDisk(file.Name())
	if err != nil {
		t.Fatal(err)
	}

	if onDisk > held {
		t.Errorf("the file takes %d bytes on disk, for some 14 KiB of data", onDisk)
	}
}

// TestContractWriterWritesWhatItIsGiven: whatever the content, wherever its
// zeros fall and however it is cut into writes, the file reads back as it.
func TestContractWriterWritesWhatItIsGiven(t *testing.T) {
	t.Parallel()

	const size = 200_000

	for seed := range uint64(40) {
		random := rand.New(rand.NewPCG(seed, 1))

		content := make([]byte, size)

		// Stretches of data and of zeros, of any length and at any place:
		// shorter than a block, across several, at either end.
		for at, data := 0, random.IntN(2) == 0; at < size; data = !data {
			length := 1 + random.IntN(20_000)
			if random.IntN(3) == 0 {
				length = 1 + random.IntN(100)
			}

			end := min(at+length, size)

			if data {
				for i := at; i < end; i++ {
					content[i] = byte(1 + random.IntN(255))
				}
			}

			at = end
		}

		file := create(t)

		writer, err := sparse.NewWriter(file, size)
		if err != nil {
			t.Fatal(err)
		}

		for rest := content; len(rest) > 0; {
			n := min(1+random.IntN(70_000), len(rest))

			written, failed := writer.Write(rest[:n])
			if failed != nil || written != n {
				t.Fatalf("seed %d: wrote %d of %d: %v", seed, written, n, failed)
			}

			rest = rest[n:]
		}

		got, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(got, content) {
			t.Errorf("seed %d: the file is not what was written (%d bytes of %d)", seed, len(got), size)
		}
	}
}

// TestContractWriterSaysAFileTheFilesystemCannotHold: "a filesystem that
// cannot hold a file that long says so here, before anything is written".
// No filesystem holds a file of a negative length.
func TestContractWriterSaysAFileTheFilesystemCannotHold(t *testing.T) {
	t.Parallel()

	if _, err := sparse.NewWriter(create(t), -1); err == nil {
		t.Error("a file of -1 bytes was sized")
	}

	closed := create(t)
	_ = closed.Close()

	if _, err := sparse.NewWriter(closed, 1); !errors.Is(err, os.ErrClosed) {
		t.Errorf("a closed file: %v", err)
	}
}

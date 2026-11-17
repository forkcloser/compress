package tarcmd_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/tarcmd"
)

// headerBlock is one header block, laid out by hand: archive/tar writes no
// sparse member. magic is the eight bytes of magic and version.
func headerBlock(name string, typeflag byte, size int, magic string) []byte {
	block := make([]byte, 512)
	copy(block[0:100], name)
	copy(block[100:108], "0000644\x00")
	copy(block[108:116], "0000000\x00")
	copy(block[116:124], "0000000\x00")
	copy(block[124:136], fmt.Sprintf("%011o\x00", size))
	copy(block[136:148], "00000000000\x00")
	block[156] = typeflag
	copy(block[257:265], magic)

	return block
}

// summed is block with its checksum field filled.
func summed(block []byte) []byte {
	copy(block[148:156], "        ")

	sum := 0
	for _, b := range block {
		sum += int(b)
	}

	copy(block[148:156], fmt.Sprintf("%06o\x00 ", sum))

	return block
}

func padded(data string) []byte {
	return append([]byte(data), make([]byte, (512-len(data)%512)%512)...)
}

// paxRecord is one extended header record, its own length first.
func paxRecord(key, value string) string {
	body := " " + key + "=" + value + "\n"
	size := len(body) + 1

	for len(fmt.Sprint(size))+len(body) != size {
		size = len(fmt.Sprint(size)) + len(body)
	}

	return fmt.Sprint(size) + body
}

const (
	ustarMagic = "ustar\x0000"
	gnuMagic   = "ustar  \x00"
)

// TestContractSparseMemberIsRefused: a sparse member states a size and
// carries a map of it; read as a file it is that size, the holes as zeros.
// Extraction refuses it, in the three PAX forms and in the old GNU one, writes
// nothing at its name, extracts the member after it and exits with failure:
// here two archives of a few blocks each state 64 MiB with no data at all.
func TestContractSparseMemberIsRefused(t *testing.T) {
	t.Parallel()

	const realSize = 64 << 20

	after := append(summed(headerBlock("after", '0', 5, ustarMagic)), padded("after")...)
	trailer := make([]byte, 1024)

	records := paxRecord("GNU.sparse.major", "1") + paxRecord("GNU.sparse.minor", "0") +
		paxRecord("GNU.sparse.name", "hole") + paxRecord("GNU.sparse.realsize", fmt.Sprint(realSize))

	// The old form: the real size is in the header, at 483, after four
	// empty map entries.
	old := headerBlock("hole", 'S', 0, gnuMagic)
	copy(old[483:495], fmt.Sprintf("%011o\x00", realSize))

	// The two PAX forms before 1.0 carry the map in the records: as one
	// record, or as an offset and a length for each piece of data.
	sized := paxRecord("GNU.sparse.size", fmt.Sprint(realSize)) + paxRecord("GNU.sparse.numblocks", "1")
	mapped := sized + paxRecord("GNU.sparse.map", "0,0")
	paired := sized + paxRecord("GNU.sparse.offset", "0") + paxRecord("GNU.sparse.numbytes", "0")

	inRecords := func(records string) []byte {
		return bytes.Join([][]byte{
			summed(headerBlock("PaxHeaders.0/hole", 'x', len(records), ustarMagic)), padded(records),
			summed(headerBlock("hole", '0', 0, ustarMagic)),
			after, trailer,
		}, nil)
	}

	for name, archive := range map[string][]byte{
		"PAX 1.0": bytes.Join([][]byte{
			summed(headerBlock("PaxHeaders.0/hole", 'x', len(records), ustarMagic)), padded(records),
			summed(headerBlock("GNUSparseFile.0/hole", '0', 512, ustarMagic)), padded("0\n"),
			after, trailer,
		}, nil),
		"PAX 0.1": inRecords(mapped),
		"PAX 0.0": inRecords(paired),
		"old GNU": bytes.Join([][]byte{summed(old), after, trailer}, nil),
	} {
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

package zipcmd_test

import (
	"encoding/binary"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/reference"
	"github.com/forkcloser/go-compress/internal/zipcmd"
)

// TestContractStdoutIsTheArchive: with "-" standard output carries the
// archive and nothing else; the entry lines go to standard error, and none
// at all under -q. The archive is whole as it stands: it ends with its end
// record, which says where the central directory is.
func TestContractStdoutIsTheArchive(t *testing.T) {
	tree(t)

	status, archive, stderr := run(t, "", "-", "d/a.txt", "d/sub/b.txt")
	if status != zipcmd.ExitOK {
		t.Fatalf("exit %d\n%s", status, stderr)
	}

	if !strings.HasPrefix(stderr, "  adding: d/a.txt (deflated 9") ||
		!strings.HasSuffix(stderr, "%)\n  adding: d/sub/b.txt (deflated 0%)\n") {
		t.Errorf("stderr %q, want the two entry lines", stderr)
	}

	if n := strings.Count(archive, "adding:"); n != 0 {
		t.Errorf("%d entry lines inside the archive", n)
	}

	// The end record with no comment: its signature, then the central
	// directory's offset 16 bytes in.
	const endLen, dirOffset = 22, 16

	if len(archive) < endLen || !strings.HasPrefix(archive[len(archive)-endLen:], "PK\x05\x06") {
		t.Fatal("standard output does not end with the archive's end record")
	}

	dir := binary.LittleEndian.Uint32([]byte(archive[len(archive)-endLen+dirOffset:]))
	if int(dir) > len(archive) || !strings.HasPrefix(archive[dir:], "PK\x01\x02") {
		t.Errorf("no central directory at offset %d, where the end record says it is", dir)
	}

	entries := readArchive(t, []byte(archive))
	if got := strings.Join(names(entries), " "); got != "d/a.txt d/sub/b.txt" {
		t.Fatalf("names %q", got)
	}

	if !strings.HasPrefix(entries[0].data, "the quick") || entries[1].data != "x\n" {
		t.Errorf("content: %q %q", entries[0].data, entries[1].data)
	}

	if status, _, stderr = run(t, "", "-q", "-", "d/a.txt"); status != zipcmd.ExitOK || stderr != "" {
		t.Errorf("-q: exit %d, stderr %q", status, stderr)
	}

	t.Run("read by Info-ZIP", func(t *testing.T) {
		theirUnzip := reference.Find(t, "unzip", "-v")

		if err := os.WriteFile("stdout.zip", []byte(archive), 0o644); err != nil {
			t.Fatal(err)
		}

		// Info-ZIP exits 1 for an archive with bytes its records do not account for.
		if out, err := exec.Command(theirUnzip, "-t", "stdout.zip").CombinedOutput(); err != nil {
			t.Errorf("%s -t: %v\n%s", theirUnzip, err, out)
		}
	})
}

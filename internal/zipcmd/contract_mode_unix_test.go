//go:build unix

package zipcmd_test

// Unix alone: Windows has no umask to create a file through, and no
// permission bits for an archive to keep.

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

// TestContractArchiveMode: a new archive is created through the umask, as
// any file is; an update leaves the archive the permissions it had, past
// the umask; and neither leaves its working file behind.
func TestContractArchiveMode(t *testing.T) {
	tree(t)

	old := syscall.Umask(0o027)

	t.Cleanup(func() { syscall.Umask(old) })

	mustRun(t, "", "-q", "a.zip", "d/a.txt")

	info, err := os.Stat("a.zip")
	if err != nil {
		t.Fatal(err)
	}

	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("a new archive under umask 027 is %v, want -rw-r-----", got)
	}

	for _, mode := range []os.FileMode{0o600, 0o664, 0o755} {
		if chErr := os.Chmod("a.zip", mode); chErr != nil {
			t.Fatal(chErr)
		}

		mustRun(t, "", "-q", "a.zip", "d/sub/b.txt")

		info, err = os.Stat("a.zip")
		if err != nil {
			t.Fatal(err)
		}

		if got := info.Mode().Perm(); got != mode {
			t.Errorf("after an update the archive is %v, it was %v", got, mode)
		}
	}

	if got := strings.Join(names(readArchiveFile(t, "a.zip")), " "); got != "d/a.txt d/sub/b.txt" {
		t.Errorf("names %q", got)
	}

	left, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range left {
		if entry.Name() != "a.zip" && entry.Name() != "d" {
			t.Errorf("%s is left beside the archive", entry.Name())
		}
	}
}

package zipcmd_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/go-compress/internal/zipcmd"
)

// TestContractUpdateKeepsUnreadable: an update whose input cannot be opened
// keeps the entry the archive already has under that name, untouched, says
// so, goes on with the other inputs and exits 18; under -o the entry kept
// still counts toward the archive's time.
func TestContractUpdateKeepsUnreadable(t *testing.T) {
	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("root reads anything, and Windows has no mode to lock a file with")
	}

	_, when := tree(t)
	mustRun(t, "", "-q", "keep.zip", "d/a.txt", "d/sub/b.txt")

	before := readArchiveFile(t, "keep.zip")

	writeFile(t, filepath.Join("d", "sub", "b.txt"), "changed\n")

	older := when.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join("d", "sub", "b.txt"), older, older); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(filepath.Join("d", "a.txt"), 0); err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr := run(t, "", "-o", "keep.zip", "d/a.txt", "d/sub/b.txt")
	if status != zipcmd.ExitInput {
		t.Errorf("exit %d, want %d", status, zipcmd.ExitInput)
	}

	if want := "\tzip warning: could not open for reading: d/a.txt\n" +
		"\tzip warning: will just copy entry over: d/a.txt\n"; stderr != want {
		t.Errorf("stderr %q, want %q", stderr, want)
	}

	if want := "updating: d/sub/b.txt (deflated 0%)\n"; stdout != want {
		t.Errorf("stdout %q, want %q", stdout, want)
	}

	after := readArchiveFile(t, "keep.zip")
	if got := strings.Join(names(after), " "); got != "d/a.txt d/sub/b.txt" {
		t.Fatalf("the archive holds %q, want \"d/a.txt d/sub/b.txt\"", got)
	}

	kept, was := after[0], before[0]
	if kept.data != was.data || kept.mode != was.mode || kept.method != was.method || !kept.when.Equal(was.when) {
		t.Errorf("d/a.txt is %+v, it was %+v", kept, was)
	}

	if after[1].data != "changed\n" {
		t.Errorf("d/sub/b.txt holds %q, want \"changed\\n\"", after[1].data)
	}

	info, err := os.Stat("keep.zip")
	if err != nil {
		t.Fatal(err)
	}

	if !info.ModTime().Equal(when) {
		t.Errorf("-o: archive time %v, want the kept entry's %v", info.ModTime(), when)
	}
}

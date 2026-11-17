package unzipcmd_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// occupied is the fixture's destination with the archive's first directory
// already there, private and dated before the archive.
func occupied(t *testing.T, out string) string {
	t.Helper()

	existing := filepath.Join(out, "d")
	if err := os.MkdirAll(existing, 0o700); err != nil {
		t.Fatal(err)
	}

	before := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(existing, before, before); err != nil {
		t.Fatal(err)
	}

	return existing
}

// TestContractExistingDirectoryUntouched: a directory entry for a directory
// that is already there creates nothing, so nothing is said of it and the
// archive's mode and time are not put on it; they go to the directories the
// run creates, as Info-ZIP's do.
func TestContractExistingDirectoryUntouched(t *testing.T) {
	t.Parallel()

	archive, out := fixture(t)
	existing := occupied(t, out)

	stdout := mustRun(t, "-o", archive, "-d", out)

	want := "Archive:  " + archive + "\n" +
		"  inflating: " + out + "/d/a.txt\n" +
		"   creating: " + out + "/d/sub\n" +
		" extracting: " + out + "/d/sub/b.txt\n" +
		"    linking: " + out + "/d/link  -> a.txt\n"
	if got := trimLines(stdout); got != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", got, want)
	}

	info, err := os.Stat(existing)
	if err != nil {
		t.Fatal(err)
	}

	// Windows has no permission bits to keep.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("the existing directory went from drwx------ to %v", info.Mode())
	}

	// Its time is that of the last entry made in it, not the archive's.
	if info.ModTime().Equal(stamp()) {
		t.Errorf("the existing directory was dated %v, the archive's time for it", info.ModTime())
	}

	sub, err := os.Stat(filepath.Join(existing, "sub"))
	if err != nil {
		t.Fatal(err)
	}

	if (runtime.GOOS != "windows" && sub.Mode().Perm() != 0o755) || !sub.ModTime().Equal(stamp()) {
		t.Errorf("the directory created: %v %v", sub.Mode(), sub.ModTime())
	}
}

// TestContractExistingDirectoryAsInfoZIP: over a directory that is already
// there, the lines, the directory's mode and whether it took the archive's
// time are the Info-ZIP unzip's. The link is left out: that tool has other
// lines for one.
func TestContractExistingDirectoryAsInfoZIP(t *testing.T) {
	t.Parallel()

	archive, _ := fixture(t)
	out := filepath.Join(t.TempDir(), "out")

	type outcome struct {
		stdout string
		mode   os.FileMode
		dated  bool
	}

	// Each run gets the directory as it was: the second removes what the
	// first extracted.
	extract := func(unzip func(args ...string) string) outcome {
		existing := occupied(t, out)
		stdout := unzip("-o", archive, "-d", out, "-x", "d/link")

		info, err := os.Stat(existing)
		if err != nil {
			t.Fatal(err)
		}

		if err := os.RemoveAll(out); err != nil {
			t.Fatal(err)
		}

		return outcome{slashless(stdout), info.Mode().Perm(), info.ModTime().Equal(stamp())}
	}

	want := extract(func(args ...string) string {
		status, stdout, stderr := theirs(t, args...)
		if status != 0 {
			t.Fatalf("Info-ZIP: exit %d\n%s", status, stderr)
		}

		return stdout
	})

	if got := extract(func(args ...string) string { return mustRun(t, args...) }); got != want {
		t.Errorf("got %+v\nInfo-ZIP %+v", got, want)
	}
}

// slashless is the lines of a run with the slash dropped that an Info-ZIP
// build may end a created directory's name with: Debian's writes it,
// Apple's does not, and neither does this command.
func slashless(stdout string) string {
	lines := strings.Split(trimLines(stdout), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "   creating: ") {
			lines[i] = strings.TrimSuffix(line, "/")
		}
	}

	return strings.Join(lines, "\n")
}

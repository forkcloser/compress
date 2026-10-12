package zipcmd_test

// The zip command from the outside: archives built from a tree and read
// back with archive/zip, updates of an existing archive, the names and
// times as Info-ZIP stores them, Info-ZIP's refusals and statuses, and
// archives exchanged with the Info-ZIP tools on PATH.

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/unzipcmd"
	"github.com/forkcloser/compress/internal/zipcmd"
)

func run(t *testing.T, stdin string, args ...string) (status int, stdout, stderr string) {
	t.Helper()

	var out, diag bytes.Buffer

	status = zipcmd.Run(args, strings.NewReader(stdin), &out, &diag, "test")

	return status, out.String(), diag.String()
}

func mustRun(t *testing.T, stdin string, args ...string) string {
	t.Helper()

	status, stdout, stderr := run(t, stdin, args...)
	if status != zipcmd.ExitOK {
		t.Fatalf("zip %s: exit %d\n%s", strings.Join(args, " "), status, stderr)
	}

	return stdout
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tree is the fixture: d/a.txt and d/sub/b.txt under a fresh directory the
// test runs in, with a fixed time on a.txt.
func tree(t *testing.T) (dir string, when time.Time) {
	t.Helper()

	dir = t.TempDir()
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "d", "a.txt"), strings.Repeat("the quick brown fox ", 20))
	writeFile(t, filepath.Join(dir, "d", "sub", "b.txt"), "x\n")

	when = time.Date(2024, time.May, 6, 7, 8, 10, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "d", "a.txt"), when, when); err != nil {
		t.Fatal(err)
	}

	return dir, when
}

type stored struct {
	name   string
	method uint16
	mode   os.FileMode
	extra  int
	when   time.Time
	data   string
}

func readArchive(t *testing.T, data []byte) []stored {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	entries := make([]stored, 0, len(reader.File))

	for _, file := range reader.File {
		content, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		if _, err := buf.ReadFrom(content); err != nil {
			t.Fatal(err)
		}

		_ = content.Close()

		entries = append(entries, stored{
			name: file.Name, method: file.Method, mode: file.Mode(), extra: len(file.Extra),
			when: file.Modified, data: buf.String(),
		})
	}

	return entries
}

func readArchiveFile(t *testing.T, path string) []stored {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return readArchive(t, data)
}

func names(entries []stored) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.name)
	}

	return out
}

// TestCreate: -r stores the tree in name order with Info-ZIP's modes,
// methods and lines, the time as the extended timestamp; a symbolic link
// is stored as its target, the mode and content both.
func TestCreate(t *testing.T) {
	_, when := tree(t)

	if err := os.Symlink("a.txt", filepath.Join("d", "link")); err != nil {
		t.Fatal(err)
	}

	stdout := mustRun(t, "", "-r", "out.zip", "d")

	// Info-ZIP would store b.txt, which deflate does not shrink; we deflate it all the same.
	want := "  adding: d/ (stored 0%)\n  adding: d/a.txt (deflated 9"
	if !strings.HasPrefix(stdout, want) || !strings.Contains(stdout, "  adding: d/sub/b.txt (deflated 0%)\n") {
		t.Errorf("stdout %q", stdout)
	}

	entries := readArchiveFile(t, "out.zip")

	got := strings.Join(names(entries), " ")
	if got != "d/ d/a.txt d/link d/sub/ d/sub/b.txt" {
		t.Fatalf("names %q", got)
	}

	link := entries[2]
	if link.mode != 0o644 || !link.mode.IsRegular() || link.data != entries[1].data {
		t.Errorf("link: %+v", link)
	}

	if entries[0].method != zip.Store || !entries[0].mode.IsDir() || entries[0].mode.Perm() != 0o755 {
		t.Errorf("d/: %+v", entries[0])
	}

	a := entries[1]
	if a.method != zip.Deflate || a.mode != 0o644 || a.extra == 0 || !a.when.Equal(when) ||
		!strings.HasPrefix(a.data, "the quick") {
		t.Errorf("a.txt: %+v", a)
	}

	if entries[4].method != zip.Deflate || entries[4].data != "x\n" {
		t.Errorf("b.txt: %+v", entries[4])
	}
}

// TestUpdate: an input replaces the entry of its name in place, the rest
// stay, a new one is appended; -o dates the archive from its latest entry,
// with or without inputs.
func TestUpdate(t *testing.T) {
	tree(t)
	mustRun(t, "", "-qr", "out.zip", "d")

	writeFile(t, filepath.Join("d", "a.txt"), "changed\n")
	writeFile(t, "new.txt", "new\n")

	stdout := mustRun(t, "", "out.zip", "d/a.txt", "new.txt")
	if stdout != "updating: d/a.txt (deflated 0%)\n  adding: new.txt (deflated 0%)\n" {
		t.Errorf("stdout %q", stdout)
	}

	entries := readArchiveFile(t, "out.zip")
	if got := strings.Join(names(entries), " "); got != "d/ d/a.txt d/sub/ d/sub/b.txt new.txt" {
		t.Errorf("names %q", got)
	}

	if entries[1].data != "changed\n" || entries[3].data != "x\n" {
		t.Errorf("content: %+v %+v", entries[1], entries[3])
	}

	when := time.Date(2030, time.January, 2, 3, 4, 6, 0, time.UTC)
	if err := os.Chtimes("new.txt", when, when); err != nil {
		t.Fatal(err)
	}

	mustRun(t, "", "-qo", "out.zip", "new.txt")

	info, err := os.Stat("out.zip")
	if err != nil {
		t.Fatal(err)
	}

	if !info.ModTime().Equal(when) {
		t.Errorf("-o: archive time %v, want %v", info.ModTime(), when)
	}

	if chErr := os.Chtimes("out.zip", time.Now(), time.Now()); chErr != nil {
		t.Fatal(chErr)
	}

	mustRun(t, "", "-o", "out.zip")

	info, err = os.Stat("out.zip")
	if err != nil {
		t.Fatal(err)
	}

	if !info.ModTime().Equal(when) {
		t.Errorf("-o alone: archive time %v, want %v", info.ModTime(), when)
	}
}

// TestStripExtra: -X leaves only the DOS fields, the local wall clock to two
// seconds; -0 stores; the archive goes to standard output for "-" and the
// names come from standard input under -@, exactly as curl-for-win packs.
func TestStripExtra(t *testing.T) {
	_, when := tree(t)

	archive := mustRun(t, "d/a.txt\n\nd/sub/b.txt\n", "--quiet", "-0", "--strip-extra", "--names-stdin", "-")

	entries := readArchive(t, []byte(archive))
	if got := strings.Join(names(entries), " "); got != "d/a.txt d/sub/b.txt" {
		t.Fatalf("names %q", got)
	}

	a := entries[0]
	if a.method != zip.Store || a.extra != 0 || a.mode != 0o644 {
		t.Errorf("a.txt: %+v", a)
	}

	wall := when.Local() //nolint:gosmopolitan // the DOS fields hold the local wall clock
	if got := a.when.Format(time.DateTime); got != wall.Format(time.DateTime) {
		t.Errorf("DOS time %s, want %s", got, wall.Format(time.DateTime))
	}
}

// TestNames: the leading "./" and "/" go, -j keeps the base name and skips
// directories, -D skips directory entries, a name repeated is refused, and
// a name with ".." is not stored.
func TestNames(t *testing.T) {
	dir, _ := tree(t)

	mustRun(t, "", "-q", "dot.zip", "./d/a.txt", filepath.Join(dir, "d", "sub", "b.txt"))

	want := "d/a.txt " + strings.TrimLeft(filepath.ToSlash(filepath.Join(dir, "d", "sub", "b.txt")), "/")
	if got := strings.Join(names(readArchiveFile(t, "dot.zip")), " "); got != want {
		t.Errorf("names %q, want %q", got, want)
	}

	mustRun(t, "", "-qrj", "junk.zip", "d")

	if got := strings.Join(names(readArchiveFile(t, "junk.zip")), " "); got != "a.txt b.txt" {
		t.Errorf("-j names %q", got)
	}

	mustRun(t, "", "-qrD", "nodirs.zip", "d")

	if got := strings.Join(names(readArchiveFile(t, "nodirs.zip")), " "); got != "d/a.txt d/sub/b.txt" {
		t.Errorf("-D names %q", got)
	}

	writeFile(t, filepath.Join("e", "a.txt"), "other\n")

	status, _, stderr := run(t, "", "-qj", "dup.zip", "d/a.txt", "e/a.txt")
	if status != zipcmd.ExitUsage || !strings.Contains(stderr, "cannot repeat names in zip file") {
		t.Errorf("repeated: exit %d, stderr %q", status, stderr)
	}

	status, _, stderr = run(t, "", "-q", "up.zip", "../"+filepath.Base(dir)+"/d/a.txt")
	if status != zipcmd.ExitNothing || !strings.Contains(stderr, "name not matched") {
		t.Errorf("..: exit %d, stderr %q", status, stderr)
	}

	mustRun(t, "", "-q", "noext", "d/a.txt")

	if _, err := os.Stat("noext.zip"); err != nil {
		t.Errorf("no extension: %v", err)
	}
}

// TestRefusals: Info-ZIP's statuses for nothing to do, a bad option, an
// archive that is not one, and an input that cannot be read.
func TestRefusals(t *testing.T) {
	tree(t)

	for _, tc := range []struct {
		args   []string
		status int
		want   string
	}{
		{[]string{"out.zip"}, zipcmd.ExitNothing, "zip error: Nothing to do! (out.zip)"},
		{[]string{"out.zip", "missing"}, zipcmd.ExitNothing, "zip warning: name not matched: missing"},
		{[]string{"--bogus", "out.zip", "d"}, zipcmd.ExitUsage, "long option 'bogus' not supported"},
		{[]string{"-y", "out.zip", "d"}, zipcmd.ExitUsage, "short option 'y' not supported"},
		{[]string{"d/a.txt", "d/sub/b.txt"}, zipcmd.ExitStructure, "Zip file structure invalid"},
		{[]string{}, zipcmd.ExitUsage, "Usage: zip"},
	} {
		status, _, stderr := run(t, "", tc.args...)
		if status != tc.status || !strings.Contains(stderr, tc.want) {
			t.Errorf("zip %v: exit %d, stderr %q, want %d %q", tc.args, status, stderr, tc.status, tc.want)
		}
	}

	status, stdout, stderr := run(t, "", "-q", "some.zip", "missing", "d/a.txt")
	if status != zipcmd.ExitOK || stdout != "" || !strings.Contains(stderr, "name not matched: missing") {
		t.Errorf("one missing: exit %d, stdout %q, stderr %q", status, stdout, stderr)
	}

	for _, flag := range []string{"-h", "-v", "--version"} {
		if got := mustRun(t, "", flag); got == "" {
			t.Errorf("%s printed nothing", flag)
		}
	}

	if os.Getuid() == 0 {
		return // root reads anything
	}

	if err := os.Chmod(filepath.Join("d", "sub", "b.txt"), 0); err != nil {
		t.Fatal(err)
	}

	status, _, stderr = run(t, "", "-qr", "locked.zip", "d")
	if status != zipcmd.ExitInput || !strings.Contains(stderr, "could not open for reading: d/sub/b.txt") {
		t.Errorf("unreadable: exit %d, stderr %q", status, stderr)
	}

	if got := strings.Join(names(readArchiveFile(t, "locked.zip")), " "); got != "d/ d/a.txt d/sub/" {
		t.Errorf("unreadable: names %q", got)
	}
}

// TestInterop: our archive through the Info-ZIP unzip on PATH, and its zip's
// archive through our unzip, when they are there.
func TestInterop(t *testing.T) {
	tree(t)

	theirUnzip, err := exec.LookPath("unzip")
	if err != nil {
		t.Skip("no unzip on PATH")
	}

	mustRun(t, "", "-qr", "ours.zip", "d")

	if out, testErr := exec.Command(theirUnzip, "-t", "ours.zip").CombinedOutput(); testErr != nil {
		t.Errorf("%s -t: %v\n%s", theirUnzip, testErr, out)
	}

	theirZip, err := exec.LookPath("zip")
	if err != nil {
		t.Skip("no zip on PATH")
	}

	if out, zipErr := exec.Command(theirZip, "-qr", "theirs.zip", "d").CombinedOutput(); zipErr != nil {
		t.Fatalf("%s: %v\n%s", theirZip, zipErr, out)
	}

	var stdout, stderr bytes.Buffer

	status := unzipcmd.Run([]string{"-q", "theirs.zip", "-d", "out"}, &stdout, &stderr, "test")
	if status != unzipcmd.ExitOK {
		t.Fatalf("unzip theirs.zip: exit %d\n%s", status, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join("out", "d", "sub", "b.txt"))
	if err != nil || string(data) != "x\n" {
		t.Errorf("their archive: %q %v", data, err)
	}
}

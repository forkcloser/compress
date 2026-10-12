package unzipcmd_test

// The unzip command from the outside: archives built with archive/zip and
// extracted, listed, tested and piped as Info-ZIP does, its statuses, the
// names that would escape the destination, and the times as stored.

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/unzipcmd"
)

func run(t *testing.T, args ...string) (status int, stdout, stderr string) {
	t.Helper()

	var out, diag bytes.Buffer

	status = unzipcmd.Run(args, &out, &diag, "test")

	return status, out.String(), diag.String()
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()

	status, stdout, stderr := run(t, args...)
	if status != unzipcmd.ExitOK {
		t.Fatalf("unzip %s: exit %d\n%s", strings.Join(args, " "), status, stderr)
	}

	return stdout
}

// member is one entry to build: a directory when name ends in "/", a
// symbolic link when link is set, a file otherwise.
type member struct {
	name    string
	data    string
	link    string
	mode    fs.FileMode
	when    time.Time
	dosOnly bool
	badCRC  bool
	store   bool
}

// stamp is the fixture's time.
func stamp() time.Time {
	return time.Date(2024, time.May, 6, 7, 8, 10, 0, time.UTC)
}

// build writes an archive of the members to path.
func build(t *testing.T, path string, members ...member) string {
	t.Helper()

	var buf bytes.Buffer

	writer := zip.NewWriter(&buf)

	for _, m := range members {
		header := &zip.FileHeader{Name: m.name, Method: zip.Deflate}

		mode := m.mode
		if mode == 0 {
			mode = 0o644
		}

		switch {
		case strings.HasSuffix(m.name, "/"):
			mode |= fs.ModeDir
			header.Method = zip.Store
		case m.link != "":
			mode |= fs.ModeSymlink
			m.data = m.link
		}

		header.SetMode(mode)

		modTime := m.when
		if modTime.IsZero() {
			modTime = stamp()
		}

		if m.dosOnly {
			// The DOS fields alone, as zip -X writes them.
			date := uint16(modTime.Day() | int(modTime.Month())<<5 | (modTime.Year()-1980)<<9)
			clock := uint16(modTime.Second()/2 | modTime.Minute()<<5 | modTime.Hour()<<11)
			header.ModifiedDate, header.ModifiedTime = date, clock //nolint:staticcheck // SA1019: deliberate
		} else {
			header.Modified = modTime
		}

		if m.store {
			header.Method = zip.Store
		}

		if m.badCRC {
			header.Method = zip.Store
			header.CRC32 = 1
			header.CompressedSize64 = uint64(len(m.data))
			header.UncompressedSize64 = uint64(len(m.data))

			raw, err := writer.CreateRaw(header)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := raw.Write([]byte(m.data)); err != nil {
				t.Fatal(err)
			}

			continue
		}

		content, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := content.Write([]byte(m.data)); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func fixture(t *testing.T) (archive, out string) {
	t.Helper()

	dir := t.TempDir()

	archive = build(t, filepath.Join(dir, "a.zip"),
		member{name: "d/", mode: 0o755},
		member{name: "d/a.txt", data: strings.Repeat("the quick brown fox ", 20), mode: 0o640},
		member{name: "d/sub/", mode: 0o755},
		member{name: "d/sub/b.txt", data: "x\n", store: true},
		member{name: "d/link", link: "a.txt", mode: 0o777},
	)

	return archive, filepath.Join(dir, "out")
}

// TestExtract: the tree lands under -d with its modes and times, Info-ZIP's
// lines on standard output; -q prints nothing.
func TestExtract(t *testing.T) {
	t.Parallel()

	archive, out := fixture(t)

	stdout := mustRun(t, archive, "-d", out)

	want := []string{
		"Archive:  " + archive,
		"   creating: " + out + "/d",
		"  inflating: " + out + "/d/a.txt",
		"   creating: " + out + "/d/sub",
		" extracting: " + out + "/d/sub/b.txt",
		"    linking: " + out + "/d/link  -> a.txt",
	}

	var got []string
	for line := range strings.SplitSeq(strings.TrimSuffix(stdout, "\n"), "\n") {
		got = append(got, strings.TrimRight(line, " "))
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("stdout:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	a, err := os.Stat(filepath.Join(out, "d", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if a.Mode().Perm() != 0o640 || !a.ModTime().Equal(stamp()) {
		t.Errorf("a.txt: %v %v", a.Mode(), a.ModTime())
	}

	if got := readFile(t, filepath.Join(out, "d", "sub", "b.txt")); got != "x\n" {
		t.Errorf("b.txt %q", got)
	}

	target, err := os.Readlink(filepath.Join(out, "d", "link"))
	if err != nil || target != "a.txt" {
		t.Errorf("link: %q %v", target, err)
	}

	d, err := os.Stat(filepath.Join(out, "d"))
	if err != nil {
		t.Fatal(err)
	}

	if d.Mode().Perm() != 0o755 || !d.ModTime().Equal(stamp()) {
		t.Errorf("d: %v %v", d.Mode(), d.ModTime())
	}

	if stdout := mustRun(t, "-qo", archive, "-d", out); stdout != "" {
		t.Errorf("-q wrote %q", stdout)
	}
}

// TestOverwrite: a file in the way is kept and reported without -o or -n,
// kept silently under -n, replaced under -o; -d attached to its cluster.
func TestOverwrite(t *testing.T) {
	t.Parallel()

	archive, out := fixture(t)
	mustRun(t, "-q", archive, "-d", out)

	a := filepath.Join(out, "d", "a.txt")
	if err := os.WriteFile(a, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, _, stderr := run(t, "-q", archive, "-d", out)
	if status != unzipcmd.ExitWarning || !strings.Contains(stderr, "replace "+a+"?") {
		t.Errorf("exit %d, stderr %q", status, stderr)
	}

	if status, _, stderr := run(t, "-qn", archive, "-d", out); status != unzipcmd.ExitOK || stderr != "" {
		t.Errorf("-n: exit %d, stderr %q", status, stderr)
	}

	if got := readFile(t, a); got != "mine" {
		t.Errorf("kept: %q", got)
	}

	mustRun(t, "-qod"+out, archive)

	if got := readFile(t, a); !strings.HasPrefix(got, "the quick") {
		t.Errorf("-o: %q", got)
	}
}

// TestList: -l is Info-ZIP's short listing, the stored wall clock.
func TestList(t *testing.T) {
	t.Parallel()

	archive, _ := fixture(t)

	stdout := mustRun(t, "-l", archive)

	want := "Archive:  " + archive + `
  Length      Date    Time    Name
---------  ---------- -----   ----
        0  05-06-2024 07:08   d/
      400  05-06-2024 07:08   d/a.txt
        0  05-06-2024 07:08   d/sub/
        2  05-06-2024 07:08   d/sub/b.txt
        5  05-06-2024 07:08   d/link
---------                     -------
      407                     5 files
`

	if stdout != want {
		t.Errorf("listing:\n%s\nwant:\n%s", stdout, want)
	}

	one := build(t, filepath.Join(t.TempDir(), "one.zip"), member{name: "x", data: "1"})

	if stdout := mustRun(t, "-l", one); !strings.HasSuffix(stdout, "        1                     1 file\n") {
		t.Errorf("singular: %q", stdout)
	}
}

// TestTestAndPipe: -t reads every entry and reports a bad CRC with status
// 2; -p writes the content alone.
func TestTestAndPipe(t *testing.T) {
	t.Parallel()

	archive, _ := fixture(t)

	stdout := mustRun(t, "-t", archive)

	if !strings.Contains(stdout, "    testing: d/a.txt                  OK\n") ||
		!strings.HasSuffix(stdout, "No errors detected in compressed data of "+archive+".\n") {
		t.Errorf("-t: %q", stdout)
	}

	if quiet := mustRun(t, "-qt", archive); strings.Contains(quiet, "testing:") {
		t.Errorf("-qt: %q", quiet)
	}

	bad := build(t, filepath.Join(t.TempDir(), "bad.zip"),
		member{name: "fine", data: "ok"}, member{name: "bad", data: "corrupt", badCRC: true})

	status, stdout, _ := run(t, "-t", bad)
	if status != unzipcmd.ExitError || !strings.Contains(stdout, "    testing: bad                      bad CRC") ||
		!strings.HasSuffix(stdout, "At least one error was detected in "+bad+".\n") {
		t.Errorf("bad CRC: exit %d, stdout %q", status, stdout)
	}

	status, _, stderr := run(t, "-q", bad, "-d", t.TempDir())
	if status != unzipcmd.ExitError || !strings.Contains(stderr, "bad CRC") {
		t.Errorf("extract bad CRC: exit %d, stderr %q", status, stderr)
	}

	if got := mustRun(t, "-p", archive, "d/sub/b.txt"); got != "x\n" {
		t.Errorf("-p: %q", got)
	}

	status, _, stderr = run(t, "-p", bad, "bad")
	if status != unzipcmd.ExitError || !strings.Contains(stderr, "bad:  bad CRC") {
		t.Errorf("-p bad CRC: exit %d, stderr %q", status, stderr)
	}
}

// TestMembers: patterns select entries, "*" across slashes; -x excludes; a
// pattern matching nothing is a caution and status 11.
func TestMembers(t *testing.T) {
	t.Parallel()

	archive, out := fixture(t)

	mustRun(t, "-q", archive, "d/sub/*", "-d", out)

	if _, err := os.Stat(filepath.Join(out, "d", "a.txt")); err == nil {
		t.Error("a.txt extracted outside the pattern")
	}

	if got := readFile(t, filepath.Join(out, "d", "sub", "b.txt")); got != "x\n" {
		t.Errorf("b.txt %q", got)
	}

	out2 := filepath.Join(t.TempDir(), "x")
	mustRun(t, "-q", archive, "-x", "d/sub/*", "d/link", "-d", out2)

	for _, absent := range []string{filepath.Join("d", "sub", "b.txt"), filepath.Join("d", "link")} {
		if _, err := os.Lstat(filepath.Join(out2, absent)); err == nil {
			t.Errorf("%s extracted despite -x", absent)
		}
	}

	if _, err := os.Stat(filepath.Join(out2, "d", "a.txt")); err != nil {
		t.Errorf("a.txt: %v", err)
	}

	status, stdout, _ := run(t, "-q", archive, "nope", "-d", t.TempDir())
	if status != unzipcmd.ExitNoMatch || !strings.Contains(stdout, "caution: filename not matched:  nope") {
		t.Errorf("no match: exit %d, stdout %q", status, stdout)
	}

	status, _, _ = run(t, "-q", archive, "d/a.txt", "nope", "-d", t.TempDir())
	if status != unzipcmd.ExitNoMatch {
		t.Errorf("partial match: exit %d", status)
	}
}

// TestSafety: names that would land outside the destination are brought
// back under it with a warning, and nothing follows a symbolic link out.
func TestSafety(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	out := filepath.Join(base, "out")
	outside := filepath.Join(base, "outside")

	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	archive := build(t, filepath.Join(base, "evil.zip"),
		member{name: "../../evil.txt", data: "e"},
		member{name: "/abs.txt", data: "a"},
		member{name: "ok/../fine.txt", data: "f"},
		member{name: "escape", link: "../outside"},
		member{name: "escape/inner.txt", data: "i"},
	)

	status, _, stderr := run(t, "-q", archive, "-d", out)
	if status != unzipcmd.ExitError {
		t.Errorf("exit %d\n%s", status, stderr)
	}

	for _, want := range []string{
		`skipped "../" path component(s) in ../../evil.txt`,
		`skipped "../" path component(s) in ok/../fine.txt`,
		"cannot create " + out + "/escape",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}

	for name, content := range map[string]string{"evil.txt": "e", "abs.txt": "a", "ok/fine.txt": "f"} {
		if got := readFile(t, filepath.Join(out, filepath.FromSlash(name))); got != content {
			t.Errorf("%s: %q", name, got)
		}
	}

	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Errorf("outside the destination: %v %v", entries, err)
	}

	for _, stray := range []string{filepath.Join(base, "evil.txt"), filepath.Join(filepath.Dir(base), "evil.txt")} {
		if _, err := os.Stat(stray); err == nil {
			t.Errorf("%s exists", stray)
		}
	}
}

// TestJunk: -j drops the directories.
func TestJunk(t *testing.T) {
	t.Parallel()

	archive, out := fixture(t)

	mustRun(t, "-qj", archive, "-d", out, "-x", "d/link")

	if got := readFile(t, filepath.Join(out, "b.txt")); got != "x\n" {
		t.Errorf("b.txt %q", got)
	}

	if _, err := os.Stat(filepath.Join(out, "d")); err == nil {
		t.Error("-j made a directory")
	}
}

// TestTimes: an entry with only the DOS fields is dated in local time, as
// Info-ZIP reads it; one with the extended timestamp is exact.
func TestTimes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zone := time.Local //nolint:gosmopolitan // the DOS fields are local time
	local := time.Date(2024, time.May, 6, 7, 8, 10, 0, zone)

	archive := build(t, filepath.Join(dir, "t.zip"),
		member{name: "dos", data: "d", dosOnly: true, when: local},
		member{name: "ext", data: "e", when: stamp()},
	)

	mustRun(t, "-q", archive, "-d", dir)

	for name, want := range map[string]time.Time{"dos": local, "ext": stamp()} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}

		if !info.ModTime().Equal(want) {
			t.Errorf("%s: %v, want %v", name, info.ModTime(), want)
		}
	}
}

// TestRefusals: Info-ZIP's statuses for the archive and the options; a
// name without ".zip" is tried with it.
func TestRefusals(t *testing.T) {
	t.Parallel()

	archive, out := fixture(t)
	dir := filepath.Dir(archive)

	if err := os.WriteFile(filepath.Join(dir, "plain"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		args   []string
		status int
		want   string
	}{
		{[]string{filepath.Join(dir, "missing")}, unzipcmd.ExitArchive, "cannot find or open"},
		{[]string{filepath.Join(dir, "plain")}, unzipcmd.ExitArchive, "End-of-central-directory signature not found"},
		{[]string{"-Y", archive}, unzipcmd.ExitUsage, "invalid option -- 'Y'"},
		{[]string{"--bogus", archive}, unzipcmd.ExitUsage, "invalid option -- '--bogus'"},
		{[]string{archive, "-d"}, unzipcmd.ExitUsage, "-d requires a directory"},
		{[]string{}, unzipcmd.ExitUsage, "Usage: unzip"},
	} {
		status, _, stderr := run(t, tc.args...)
		if status != tc.status || !strings.Contains(stderr, tc.want) {
			t.Errorf("unzip %v: exit %d, stderr %q, want %d %q", tc.args, status, stderr, tc.status, tc.want)
		}
	}

	mustRun(t, "-q", strings.TrimSuffix(archive, ".zip"), "-d", out)

	if _, err := os.Stat(filepath.Join(out, "d", "a.txt")); err != nil {
		t.Errorf("without .zip: %v", err)
	}

	if got := mustRun(t, "-h"); !strings.Contains(got, "Usage: unzip") {
		t.Errorf("-h: %q", got)
	}
}

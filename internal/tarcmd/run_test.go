package tarcmd_test

// The command's contract from the outside: GNU tar's grammar for the options
// the rig uses, archives that GNU and BSD tar read, their archives extracted
// faithfully, nothing ever written outside the destination, and GNU's exit
// statuses and diagnostics.

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/constants"
	"github.com/forkcloser/compress/internal/reference"
	"github.com/forkcloser/compress/internal/tarcmd"
)

// run invokes the command with args and stdin, returning its status and the
// two output streams.
func run(t *testing.T, stdin []byte, args ...string) (status int, stdout, stderr string) {
	t.Helper()

	var out, diag bytes.Buffer

	status = tarcmd.Run(args, bytes.NewReader(stdin), &out, &diag, "test")

	return status, out.String(), diag.String()
}

// mustRun is run for an invocation that must succeed.
func mustRun(t *testing.T, stdin []byte, args ...string) (stdout, stderr string) {
	t.Helper()

	status, out, diag := run(t, stdin, args...)
	if status != tarcmd.ExitOK {
		t.Fatalf("tar %s: exit %d\n%s", strings.Join(args, " "), status, diag)
	}

	return out, diag
}

// tree is the source every round trip starts from: nested directories, a
// file with the execute bit, an empty directory, and a symlink.
func tree(t *testing.T) string {
	t.Helper()

	src := filepath.Join(t.TempDir(), "src")

	for _, dir := range []string{"sub/deep", "empty"} {
		if err := os.MkdirAll(filepath.Join(src, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(t, filepath.Join(src, "hello.txt"), "hello\n", 0o644)
	writeFile(t, filepath.Join(src, "sub", "run.sh"), "#!/bin/sh\n", 0o755)
	writeFile(t, filepath.Join(src, "sub", "deep", "data.bin"), strings.Repeat("x", 70000), 0o600)

	if err := os.Symlink("hello.txt", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	return src
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}

	// Past the umask: the mode is what the test compares against.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// names lists an archive's entry names, in order, through archive/tar.
func names(t *testing.T, archive []byte) []string {
	t.Helper()

	var out []string

	reader := tar.NewReader(bytes.NewReader(archive))

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return out
		}

		if err != nil {
			t.Fatal(err)
		}

		out = append(out, header.Name)
	}
}

// assertTree checks dst holds what tree built, as extracted by tar.
func assertTree(t *testing.T, dst string) {
	t.Helper()

	if got := readFile(t, filepath.Join(dst, "hello.txt")); got != "hello\n" {
		t.Errorf("hello.txt: %q", got)
	}

	if got := readFile(t, filepath.Join(dst, "sub", "deep", "data.bin")); len(got) != 70000 {
		t.Errorf("data.bin: %d bytes", len(got))
	}

	info, err := os.Stat(filepath.Join(dst, "sub", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}

	// Windows has no execute bit to keep.
	if runtime.GOOS != "windows" && info.Mode()&0o100 == 0 {
		t.Errorf("run.sh lost its execute bit: %v", info.Mode())
	}

	if empty, statErr := os.Stat(filepath.Join(dst, "empty")); statErr != nil || !empty.IsDir() {
		t.Errorf("empty directory: %v %v", empty, statErr)
	}

	target, linkErr := os.Readlink(filepath.Join(dst, "link"))
	if linkErr != nil || target != "hello.txt" {
		t.Errorf("link: %q %v", target, err)
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	src := tree(t)
	archive := filepath.Join(t.TempDir(), "a.tar")
	dst := t.TempDir()

	mustRun(t, nil, "-cf", archive, "-C", src, ".")
	mustRun(t, nil, "-xf", archive, "-C", dst)
	assertTree(t, dst)

	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}

	got := names(t, data)
	if len(got) == 0 || got[0] != "./" {
		t.Fatalf("entries: %q", got)
	}

	if !slices.Contains(got, "./sub/deep/data.bin") || !slices.Contains(got, "./link") {
		t.Errorf("entries: %q", got)
	}
}

// TestOldStyleBundle: the first argument as a dashless bundle, the way the
// rig's scripts type it, letters taking their values in order.
func TestOldStyleBundle(t *testing.T) {
	t.Parallel()

	src := tree(t)
	archive := filepath.Join(t.TempDir(), "a.tgz")
	dst := t.TempDir()

	// Without --sort the directory's own order holds, so the names are
	// compared as a set; the listing then repeats the creation's order.
	verbose, _ := mustRun(t, nil, "cvzf", archive, "-C", src, "hello.txt", "sub")

	got := strings.Split(strings.TrimSuffix(verbose, "\n"), "\n")
	slices.Sort(got)

	if want := []string{"hello.txt", "sub/", "sub/deep/", "sub/deep/data.bin", "sub/run.sh"}; !slices.Equal(got, want) {
		t.Errorf("verbose names %q, want %q", got, want)
	}

	mustRun(t, nil, "xf", archive, "-C", dst)

	if got := readFile(t, filepath.Join(dst, "sub", "run.sh")); got != "#!/bin/sh\n" {
		t.Errorf("run.sh: %q", got)
	}

	listing, _ := mustRun(t, nil, "tf", archive)
	if listing != verbose {
		t.Errorf("listing:\n%s\nverbose:\n%s", listing, verbose)
	}
}

// TestCompression: every compression flag round-trips, an archive read
// without a flag is recognized by its magic, -a picks by suffix, and bzip2
// is read but not written.
func TestCompression(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		flag   string
		suffix string
		magic  []byte
	}{
		{"-z", ".tar.gz", []byte{0x1F, 0x8B}},
		{"-J", ".tar.xz", []byte{0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00}},
		{"--zstd", ".tar.zst", []byte{0x28, 0xB5, 0x2F, 0xFD}},
		{"--lz4", ".tar.lz4", []byte{0x04, 0x22, 0x4D, 0x18}},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			t.Parallel()

			src := tree(t)

			for _, args := range [][]string{{"-c", tc.flag, "-f"}, {"-caf"}} {
				archive := filepath.Join(t.TempDir(), "a"+tc.suffix)
				dst := t.TempDir()

				mustRun(t, nil, append(args, archive, "-C", src, ".")...)

				data, err := os.ReadFile(archive)
				if err != nil {
					t.Fatal(err)
				}

				if !bytes.HasPrefix(data, tc.magic) {
					t.Fatalf("%v: archive starts with %x, want %x", args, data[:6], tc.magic)
				}

				mustRun(t, nil, "-xf", archive, "-C", dst) // no flag: by magic
				assertTree(t, dst)

				dst = t.TempDir()

				mustRun(t, nil, "-x", tc.flag, "-f", archive, "-C", dst)
				assertTree(t, dst)
			}
		})
	}

	t.Run("bzip2", func(t *testing.T) {
		t.Parallel()

		status, _, stderr := run(t, nil, "-cjf", filepath.Join(t.TempDir(), "a.tbz"), "-C", tree(t), ".")
		if status != tarcmd.ExitError || !strings.Contains(stderr, "bzip2 compression is not supported") {
			t.Fatalf("exit %d, stderr %q", status, stderr)
		}

		// Reading: the fixture is a bzip2 stream of a plain text, not an
		// archive; what matters is that the decompressor is reached.
		fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "payload.bz2"))
		if err != nil {
			t.Fatal(err)
		}

		status, _, stderr = run(t, fixture, "-tf", "-")
		if status != tarcmd.ExitError || strings.Contains(stderr, "not supported") {
			t.Fatalf("exit %d, stderr %q", status, stderr)
		}
	})
}

func TestStripComponents(t *testing.T) {
	t.Parallel()

	src := t.TempDir()

	if err := os.MkdirAll(filepath.Join(src, "pkg-1.0", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(src, "pkg-1.0", "bin", "tool"), "tool", 0o755)
	writeFile(t, filepath.Join(src, "pkg-1.0", "README"), "readme", 0o644)

	archive := filepath.Join(t.TempDir(), "a.tar")
	mustRun(t, nil, "-cf", archive, "-C", src, "pkg-1.0")

	dst := t.TempDir()
	mustRun(t, nil, "-xf", archive, "-C", dst, "--strip-components=1")

	if got := readFile(t, filepath.Join(dst, "bin", "tool")); got != "tool" {
		t.Errorf("bin/tool: %q", got)
	}

	if _, err := os.Stat(filepath.Join(dst, "pkg-1.0")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("pkg-1.0 still there: %v", err)
	}

	// Deeper than the archive: every entry is skipped, nothing fails.
	dst = t.TempDir()
	mustRun(t, nil, "--strip-components", "3", "-xf", archive, "-C", dst)

	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) != 0 {
		t.Errorf("entries after stripping everything: %v %v", entries, err)
	}
}

// TestLongListing: -tv in GNU's layout, as the rig's scripts parse it: the
// type as the first character, the name last, a symlink's target after " -> ".
func TestLongListing(t *testing.T) {
	t.Parallel()

	src := tree(t)
	archive := filepath.Join(t.TempDir(), "a.tar")
	mustRun(
		t,
		nil,
		"-cf",
		archive,
		"-C",
		src,
		".",
		"--mtime=2025-01-01 00:00:00",
		"--owner=0",
		"--group=0",
		"--numeric-owner",
	)

	stdout, _ := mustRun(t, nil, "-tvf", archive)

	line := regexp.MustCompile(`^([-dlhcbp])[rwxsStT-]{9} 0/0 +(\d+) 2025-01-01 00:00 (\S+)( -> (\S+))?$`)

	var types []string

	for l := range strings.SplitSeq(strings.TrimSuffix(stdout, "\n"), "\n") {
		m := line.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("line %q does not match", l)
		}

		types = append(types, m[1]+" "+m[3]+" "+m[5])
	}

	for _, want := range []string{"d ./ ", "- ./hello.txt ", "l ./link hello.txt", "- ./sub/run.sh "} {
		if !slices.Contains(types, want) {
			t.Errorf("%q not among %q", want, types)
		}
	}
}

// TestDeterministic: the flags curl-for-win packages with produce the same
// bytes twice, in ustar, with the owner, group and mtime forced.
func TestDeterministic(t *testing.T) {
	t.Parallel()

	src := tree(t)
	list := filepath.Join(t.TempDir(), "files")
	writeFile(t, list, "sub\nsub/deep\nsub/deep/data.bin\nsub/run.sh\nhello.txt\n", 0o644)

	build := func() []byte {
		// -T reads its names where it stands, under the -C in effect there.
		stdout, _ := mustRun(t, nil, "--create", "--format=ustar", "--owner=0", "--group=0", "--numeric-owner",
			"--sort=name", "--no-recursion", "-C", src, "--files-from", list, "--mtime=@1700000000")

		return []byte(stdout)
	}

	first, second := build(), build()
	if !bytes.Equal(first, second) {
		t.Fatal("two runs differ")
	}

	reader := tar.NewReader(bytes.NewReader(first))

	var got []string

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		if header.Format != tar.FormatUSTAR {
			t.Errorf("%s: format %v", header.Name, header.Format)
		}

		if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
			t.Errorf("%s: owner %d:%d %q:%q", header.Name, header.Uid, header.Gid, header.Uname, header.Gname)
		}

		if !header.ModTime.Equal(time.Unix(1700000000, 0)) {
			t.Errorf("%s: mtime %v", header.Name, header.ModTime)
		}

		got = append(got, header.Name)
	}

	// --no-recursion: the list's own order, directories as bare entries.
	want := []string{"sub/", "sub/deep/", "sub/deep/data.bin", "sub/run.sh", "hello.txt"}
	if !slices.Equal(got, want) {
		t.Errorf("entries %q, want %q", got, want)
	}
}

// TestSortName: --sort=name orders every directory's children bytewise;
// without it, the directory's own order holds, whatever it is.
func TestSortName(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	for _, name := range []string{"b", "a", "c"} {
		writeFile(t, filepath.Join(src, name), name, 0o644)
	}

	stdout, _ := mustRun(t, nil, "-c", "--sort=name", "-C", src, ".")

	if got := names(t, []byte(stdout)); !slices.Equal(got, []string{"./", "./a", "./b", "./c"}) {
		t.Errorf("entries %q", got)
	}
}

// TestDirectoryChaining: on create, -C applies to the members after it, and
// a second relative -C is relative to the first.
func TestDirectoryChaining(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "one", "two"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(base, "one", "x"), "x", 0o644)
	writeFile(t, filepath.Join(base, "one", "two", "y"), "y", 0o644)

	stdout, _ := mustRun(t, nil, "-c", "-C", base, "-C", "one", "x", "-C", "two", "y")

	if got := names(t, []byte(stdout)); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("entries %q", got)
	}
}

// TestStdio: the archive on stdout with the verbose names on stderr, and
// the archive from stdin.
func TestStdio(t *testing.T) {
	t.Parallel()

	src := tree(t)

	stdout, stderr := mustRun(t, nil, "-cvf", "-", "-C", src, "hello.txt")
	if !strings.HasPrefix(stdout, "hello.txt") || stderr != "hello.txt\n" {
		t.Fatalf("stdout %d bytes, stderr %q", len(stdout), stderr)
	}

	dst := t.TempDir()
	mustRun(t, []byte(stdout), "-x", "-C", dst)

	if got := readFile(t, filepath.Join(dst, "hello.txt")); got != "hello\n" {
		t.Errorf("hello.txt: %q", got)
	}
}

// TestUsage: what the parser refuses, and as what.
func TestUsage(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-cf", "a.tar", "--bogus"}, "unrecognized option '--bogus'"},
		{[]string{"-q"}, "invalid option -- 'q'"},
		{[]string{"-cf"}, "option requires an argument -- 'f'"},
		{[]string{"-c"}, "Cowardly refusing to create an empty archive"},
		{[]string{"-cx", "x"}, "more than one '-Acdtrux'"},
		{[]string{"-f", "a.tar"}, "You must specify one of the '-Acdtrux'"},
		{[]string{"-cf", "a", "--strip-components=x", "y"}, "Invalid number of components"},
		{[]string{"-cf", "a", "--format=v7", "y"}, "unknown archive format 'v7'"},
		{[]string{"-cf", "a", "--mtime=yesterday", "y"}, "Invalid mtime"},
		{[]string{"-cf", "a", "--owner=nosuchuser-for-sure", "y"}, "Invalid owner or group"},
		{[]string{"-xf", "a", "member"}, "subset of members is not supported"},
		{[]string{"-cf", "a", "-I"}, "option requires an argument -- 'I'"},
		{[]string{"-cf", "a", "--use-compress-program=", "y"}, "requires a program"},
	} {
		status, _, stderr := run(t, nil, tc.args...)
		if status != tarcmd.ExitUsage || !strings.Contains(stderr, tc.want) {
			t.Errorf("tar %v: exit %d, stderr %q, want %q", tc.args, status, stderr, tc.want)
		}

		if !strings.Contains(stderr, "Try 'tar --help'") {
			t.Errorf("tar %v: no help hint in %q", tc.args, stderr)
		}
	}

	for _, flag := range []string{"--help", "--version"} {
		status, stdout, _ := run(t, nil, flag)
		if status != tarcmd.ExitOK || stdout == "" {
			t.Errorf("%s: exit %d, stdout %q", flag, status, stdout)
		}
	}
}

// TestMemberErrors: a member that cannot be read is reported, the others
// are archived, and the status says a failure happened.
func TestMemberErrors(t *testing.T) {
	t.Parallel()

	src := tree(t)
	archive := filepath.Join(t.TempDir(), "a.tar")

	status, _, stderr := run(t, nil, "-cf", archive, "-C", src, "missing", "hello.txt")
	if status != tarcmd.ExitError {
		t.Fatalf("exit %d", status)
	}

	for _, want := range []string{"tar: missing: Cannot stat", "Exiting with failure status"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q lacks %q", stderr, want)
		}
	}

	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}

	if got := names(t, data); !slices.Equal(got, []string{"hello.txt"}) {
		t.Errorf("entries %q", got)
	}

	status, _, stderr = run(t, nil, "-xf", filepath.Join(src, "nonexistent.tar"), "-C", src)
	if status != tarcmd.ExitError || !strings.Contains(stderr, "Cannot open") {
		t.Errorf("exit %d, stderr %q", status, stderr)
	}

	// An absolute member is found where it says, whatever -C is in force.
	stdout, stderr := mustRun(t, nil, "-c", "-C", t.TempDir(), filepath.Join(src, "hello.txt"))
	// A Windows absolute path starts with a drive letter, not a slash.
	if runtime.GOOS != "windows" && !strings.Contains(stderr, "Removing leading") {
		t.Errorf("stderr %q", stderr)
	}

	want := strings.TrimLeft(filepath.ToSlash(filepath.Join(src, "hello.txt")), "/")
	if got := names(t, []byte(stdout)); !slices.Equal(got, []string{want}) {
		t.Errorf("entries %q, want %q", got, want)
	}

	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		return // root reads anything, and Windows has no mode to lock a file with
	}

	// A member that cannot be read is reported and skipped; the archive
	// carries the rest and ends properly.
	writeFile(t, filepath.Join(src, "locked"), "secret", 0o000)

	status, stdout, stderr = run(t, nil, "-c", "--sort=name", "-C", src, ".")
	if status != tarcmd.ExitError || !strings.Contains(stderr, "tar: ./locked: Cannot open: permission denied") {
		t.Errorf("exit %d, stderr %q", status, stderr)
	}

	got := names(t, []byte(stdout))
	if slices.Contains(got, "./locked") || !slices.Contains(got, "./hello.txt") || len(got) < 3 {
		t.Errorf("entries %q", got)
	}
}

// crafted builds an archive from headers, the content of a regular file
// being its name.
func crafted(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()

	var buf bytes.Buffer

	writer := tar.NewWriter(&buf)

	for _, h := range headers {
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(h.Name))
		}

		if err := writer.WriteHeader(h); err != nil {
			t.Fatal(err)
		}

		if h.Typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte(h.Name)); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// TestSafety: a name with "..", a symlink pointing out and an entry through
// it, and an absolute name: nothing lands beside the destination, the
// absolute name is relocated with a warning, the others are refused.
func TestSafety(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	dst := filepath.Join(parent, "dst")

	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	archive := crafted(t,
		&tar.Header{Name: "../escape", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "/abs", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "out", Typeflag: tar.TypeSymlink, Linkname: "../../", Mode: 0o777},
		&tar.Header{Name: "out/through", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "fine", Typeflag: tar.TypeReg, Mode: 0o644},
	)

	status, _, stderr := run(t, archive, "-xf", "-", "-C", dst)
	if status != tarcmd.ExitError {
		t.Fatalf("exit %d\n%s", status, stderr)
	}

	for _, want := range []string{"Member name contains '..'", "Removing leading `/'", "out/through"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q lacks %q", stderr, want)
		}
	}

	if got := readFile(t, filepath.Join(dst, "abs")); got != "/abs" {
		t.Errorf("abs: %q", got)
	}

	if got := readFile(t, filepath.Join(dst, "fine")); got != "fine" {
		t.Errorf("fine: %q", got)
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if entry.Name() != "dst" {
			t.Errorf("%q landed beside the destination", entry.Name())
		}
	}
}

// TestEntryTypes: hard links become links, a fifo is refused and reported,
// a file over an existing symlink replaces the link rather than following
// it, and -p restores the exact mode while -m leaves the time alone.
func TestEntryTypes(t *testing.T) {
	t.Parallel()

	dst := t.TempDir()
	writeFile(t, filepath.Join(dst, "elsewhere"), "untouched", 0o644)

	if err := os.Symlink("elsewhere", filepath.Join(dst, "replaced")); err != nil {
		t.Fatal(err)
	}

	when := time.Date(2020, time.February, 3, 4, 5, 6, 0, time.UTC)

	// The sticky directory and setuid file carry what GNU tar stores for
	// them, which os.Root refuses at creation: -p restores the bits afterwards.
	archive := crafted(t,
		&tar.Header{Name: "orig", Typeflag: tar.TypeReg, Mode: 0o640, ModTime: when},
		&tar.Header{Name: "hard", Typeflag: tar.TypeLink, Linkname: "orig", Mode: 0o640},
		&tar.Header{Name: "fifo", Typeflag: tar.TypeFifo, Mode: 0o644},
		&tar.Header{Name: "replaced", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "sticky", Typeflag: tar.TypeDir, Mode: 0o1777, ModTime: when},
		&tar.Header{Name: "sticky/inner", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "suid", Typeflag: tar.TypeReg, Mode: 0o4755},
	)

	status, _, stderr := run(t, archive, "-xpf", "-", "-C", dst)
	if status != tarcmd.ExitError || !strings.Contains(stderr, "tar: fifo: Cannot mknod") {
		t.Fatalf("exit %d\n%s", status, stderr)
	}

	// Nothing else fails, except the setuid bit where the platform denies it
	// to a plain user (macOS): GNU tar reports that the same way.
	for line := range strings.SplitSeq(strings.TrimSuffix(stderr, "\n"), "\n") {
		if !strings.Contains(line, "fifo") && !strings.Contains(line, "Exiting") &&
			!strings.HasPrefix(line, "tar: suid: Cannot change mode") {
			t.Errorf("unexpected: %s", line)
		}
	}

	// Windows has none of these bits: the entries land, their modes are its.
	posix := runtime.GOOS != "windows"

	sticky, err := os.Stat(filepath.Join(dst, "sticky"))
	if err != nil || (posix && sticky.Mode()&(fs.ModePerm|fs.ModeSticky) != 0o777|fs.ModeSticky) {
		t.Errorf("-p: sticky directory %v %v", sticky, err)
	}

	// The platform may refuse the setuid bit to a plain user: the entry lands
	// either way, and when the chmod was refused its mode is the umask's, as
	// under GNU tar, since the refused call is the one that would set it.
	suid, err := os.Stat(filepath.Join(dst, "suid"))
	if err != nil {
		t.Fatal(err)
	}

	if posix && !strings.Contains(stderr, "tar: suid: Cannot change mode") && suid.Mode().Perm() != 0o755 {
		t.Errorf("-p: setuid file mode %v", suid.Mode())
	}

	// Without -p the special bits are not restored, and the entries land.
	plain := t.TempDir()

	if status, _, stderr := run(t, archive, "-xf", "-", "-C", plain); strings.Count(stderr, "tar: ") != 2 {
		t.Fatalf("without -p, more than the fifo failed: exit %d\n%s", status, stderr)
	}

	sticky, err = os.Stat(filepath.Join(plain, "sticky"))
	if err != nil || sticky.Mode()&fs.ModeSticky != 0 {
		t.Errorf("sticky directory without -p: %v %v", sticky, err)
	}

	if _, statErr := os.Stat(filepath.Join(plain, "suid")); statErr != nil {
		t.Errorf("setuid file without -p: %v", statErr)
	}

	orig, err := os.Stat(filepath.Join(dst, "orig"))
	if err != nil {
		t.Fatal(err)
	}

	hard, err := os.Stat(filepath.Join(dst, "hard"))
	if err != nil || !os.SameFile(orig, hard) {
		t.Errorf("hard link: %v %v", hard, err)
	}

	if posix && orig.Mode().Perm() != 0o640 {
		t.Errorf("-p: mode %v", orig.Mode())
	}

	if !orig.ModTime().Equal(when) {
		t.Errorf("mtime %v, want %v", orig.ModTime(), when)
	}

	if got := readFile(t, filepath.Join(dst, "elsewhere")); got != "untouched" {
		t.Errorf("the symlink was followed: elsewhere is %q", got)
	}

	if got := readFile(t, filepath.Join(dst, "replaced")); got != "replaced" {
		t.Errorf("replaced: %q", got)
	}

	// -m: the time is now, not the archive's.
	dst = t.TempDir()
	mustRun(
		t,
		crafted(t, &tar.Header{Name: "orig", Typeflag: tar.TypeReg, Mode: 0o644, ModTime: when}),
		"-xmf",
		"-",
		"-C",
		dst,
	)

	info, err := os.Stat(filepath.Join(dst, "orig"))
	if err != nil || info.ModTime().Equal(when) {
		t.Errorf("-m: %v %v", info, err)
	}
}

// TestInterop: archives exchanged with the tar every host has.
func TestInterop(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the tar on a Windows runner's PATH is Git for Windows' tar.exe, which fails on these paths " +
			"(exit 2 and 128): not a tar this package exchanges archives with")
	}

	system := reference.Find(t, "tar", "--version")

	src := tree(t)

	t.Run("theirs reads ours", func(t *testing.T) {
		t.Parallel()

		archive := filepath.Join(t.TempDir(), "ours.tar.gz")
		mustRun(t, nil, "-czf", archive, "-C", src, ".")

		dst := t.TempDir()

		out, err := exec.Command(system, "-xzf", archive, "-C", dst).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", system, err, out)
		}

		assertTree(t, dst)
	})

	t.Run("ours reads theirs", func(t *testing.T) {
		t.Parallel()

		archive := filepath.Join(t.TempDir(), "theirs.tar")

		out, err := exec.Command(system, "-cf", archive, "-C", src, ".").CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", system, err, out)
		}

		dst := t.TempDir()
		mustRun(t, nil, "-xf", archive, "-C", dst)
		assertTree(t, dst)
	})
}

// TestLargeFile: content longer than every buffer in the path, through xz,
// back whole.
func TestLargeFile(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	want := strings.Repeat("0123456789abcdef", 3*constants.MiB/16+5)
	writeFile(t, filepath.Join(src, "big"), want, 0o644)

	archive := filepath.Join(t.TempDir(), "big.tar.xz")
	mustRun(t, nil, "-cJf", archive, "-C", src, "big")

	dst := t.TempDir()
	mustRun(t, nil, "-xf", archive, "-C", dst)

	if got := readFile(t, filepath.Join(dst, "big")); got != want {
		t.Errorf("%d bytes back, want %d", len(got), len(want))
	}
}

// TestActionsCache: the commands actions/cache runs on Linux and macOS, GNU
// tar's grammar. Its manifest names the cached paths relative to the
// workspace, so a path outside it is "../…"; -P keeps those names and the
// absolute ones, and extraction puts every path back where it was.
func TestActionsCache(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("actions/cache runs Windows' own tar.exe on Windows, never this one")
	}

	top := t.TempDir()
	workspace := filepath.Join(top, "work", "repo", "repo")
	outside := filepath.Join(top, "home", ".cache", "go-build")
	absolute := filepath.Join(top, "go", "pkg", "mod")
	inside := filepath.Join(workspace, ".build", "cache")

	for _, dir := range []string{outside, absolute, inside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		writeFile(t, filepath.Join(dir, "entry"), dir, 0o644)
	}

	relative, err := filepath.Rel(workspace, outside)
	if err != nil {
		t.Fatal(err)
	}

	scratch := t.TempDir()
	manifest := filepath.Join(scratch, "manifest.txt")
	writeFile(t, manifest, relative+"\n.build/cache\n"+absolute+"\n", 0o644)

	archive := filepath.Join(scratch, "cache.tzst")

	mustRun(t, nil, "--posix", "-cf", archive, "--exclude", archive, "-P", "-C", workspace,
		"--files-from", manifest, "--use-compress-program", "zstdmt")

	if head := readFile(t, archive); !strings.HasPrefix(head, "\x28\xb5\x2f\xfd") {
		t.Fatalf("zstdmt: the archive is not a zstd stream: % x", head[:4])
	}

	listing, _ := mustRun(t, nil, "-tf", archive, "-P", "--use-compress-program", "unzstd")
	for _, want := range []string{
		filepath.ToSlash(relative) + "/entry",
		".build/cache/entry",
		filepath.ToSlash(absolute) + "/entry",
	} {
		if !strings.Contains(listing, want+"\n") {
			t.Errorf("listing lacks %q:\n%s", want, listing)
		}
	}

	for _, dir := range []string{outside, absolute, inside} {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}

	mustRun(t, nil, "-xf", archive, "-P", "-C", workspace, "--use-compress-program", "unzstd",
		"--delay-directory-restore")

	for _, dir := range []string{outside, absolute, inside} {
		if got := readFile(t, filepath.Join(dir, "entry")); got != dir {
			t.Errorf("%s/entry: %q", dir, got)
		}
	}

	t.Run("GNU tar", func(t *testing.T) {
		t.Parallel()

		system := reference.Find(t, "tar", "--version")

		version, err := exec.Command(system, "--version").Output()
		if err != nil || !strings.Contains(string(version), "GNU tar") {
			t.Skip("the tar on PATH is not GNU tar, the one actions/cache runs on Linux")
		}

		for _, program := range []string{"zstdmt", "unzstd"} {
			if _, err := exec.LookPath(program); err != nil {
				t.Skipf("%s not on PATH", program)
			}
		}

		dst := filepath.Join(t.TempDir(), "work")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}

		mine := filepath.Join(t.TempDir(), "mine.tzst")
		mustRun(t, nil, "--posix", "-cf", mine, "-C", workspace, "--use-compress-program", "zstdmt", ".build")

		if out, err := exec.Command(system, "-xf", mine, "-P", "-C", dst, "--use-compress-program", "unzstd").
			CombinedOutput(); err != nil {
			t.Fatalf("GNU tar reads ours: %v\n%s", err, out)
		}

		if got := readFile(t, filepath.Join(dst, ".build", "cache", "entry")); got != inside {
			t.Errorf("GNU tar extracted %q", got)
		}

		theirs := filepath.Join(t.TempDir(), "theirs.tzst")
		if out, err := exec.Command(system, "--posix", "-cf", theirs, "-P", "-C", workspace,
			"--use-compress-program", "zstdmt", ".build").CombinedOutput(); err != nil {
			t.Fatalf("GNU tar: %v\n%s", err, out)
		}

		back := t.TempDir()
		mustRun(t, nil, "-xf", theirs, "-P", "-C", back, "--use-compress-program", "unzstd")

		if got := readFile(t, filepath.Join(back, ".build", "cache", "entry")); got != inside {
			t.Errorf("ours extracted %q", got)
		}
	})
}

// TestExclude: GNU's unanchored --exclude, on create and on extract; a
// pattern naming a directory excludes what is under it.
func TestExclude(t *testing.T) {
	t.Parallel()

	src := tree(t)

	archive, _ := mustRun(t, nil, "-cf", "-", "-C", src, "--exclude", "*.bin", "--exclude", "empty", ".")
	for _, name := range names(t, []byte(archive)) {
		if strings.HasSuffix(name, ".bin") || strings.Contains(name, "empty") {
			t.Errorf("create kept %q", name)
		}
	}

	full, _ := mustRun(t, nil, "-cf", "-", "-C", src, ".")

	dst := t.TempDir()
	mustRun(t, []byte(full), "-xf", "-", "-C", dst, "--exclude=sub")

	if _, err := os.Stat(filepath.Join(dst, "sub")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("extract kept sub: %v", err)
	}

	if got := readFile(t, filepath.Join(dst, "hello.txt")); got != "hello\n" {
		t.Errorf("hello.txt: %q", got)
	}
}

// TestPosix: --posix is --format=posix.
func TestPosix(t *testing.T) {
	t.Parallel()

	src := tree(t)
	fixed := []string{"-C", src, "--sort=name", "--mtime=@0", "--owner=0", "--group=0", "--numeric-owner", "."}

	posix, _ := mustRun(t, nil, append([]string{"-cf", "-", "--posix"}, fixed...)...)
	format, _ := mustRun(t, nil, append([]string{"-cf", "-", "--format=posix"}, fixed...)...)

	if posix != format {
		t.Error("--posix and --format=posix wrote different archives")
	}
}

// TestCompressProgram: a program the command does not know is a filter it
// runs, with -d to read, and its failure is the command's.
func TestCompressProgram(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the filter is a shell script")
	}

	filter := filepath.Join(t.TempDir(), "passthrough")
	writeFile(t, filter, "#!/bin/sh\nif [ \"$1\" = -d ]; then shift; fi\nexec cat\n", 0o755)

	src := tree(t)
	archive := filepath.Join(t.TempDir(), "through.tar")

	mustRun(t, nil, "-cf", archive, "-I", filter, "-C", src, ".")

	if got := names(t, []byte(readFile(t, archive))); !slices.Contains(got, "./hello.txt") {
		t.Fatalf("the filter's output is not the archive: %v", got)
	}

	dst := t.TempDir()
	mustRun(t, nil, "-xf", archive, "--use-compress-program="+filter, "-C", dst)
	assertTree(t, dst)

	status, _, stderr := run(t, nil, "-cf", filepath.Join(t.TempDir(), "x.tar"), "-I", "false", "-C", src, ".")
	if status != tarcmd.ExitError || !strings.Contains(stderr, "Child returned status 1") {
		t.Errorf("a failing filter: exit %d, %q", status, stderr)
	}
}

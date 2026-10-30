package xzcmd_test

// The xz command from the outside: files compressed beside themselves and
// removed, decompressed back by suffix, standard streams when no file is
// named, xz's refusals, and streams the xz every host has can read.

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/xz"

	"github.com/forkcloser/compress/internal/filecodec"
	"github.com/forkcloser/compress/internal/reference"
	"github.com/forkcloser/compress/internal/xzcmd"
)

func run(t *testing.T, name string, stdin []byte, args ...string) (status int, stdout, stderr string) {
	t.Helper()

	var out, diag bytes.Buffer

	status = xzcmd.Run(name, args, bytes.NewReader(stdin), &out, &diag, "test")

	return status, out.String(), diag.String()
}

func mustRun(t *testing.T, name string, stdin []byte, args ...string) string {
	t.Helper()

	status, stdout, stderr := run(t, name, stdin, args...)
	if status != filecodec.ExitOK {
		t.Fatalf("%s %s: exit %d\n%s", name, strings.Join(args, " "), status, stderr)
	}

	return stdout
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}

	// Past the umask: the mode is what the test compares against.
	if err := os.Chmod(path, 0o640); err != nil {
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

// decode reads an xz stream through the library, as a check independent of
// the command.
func decode(t *testing.T, data []byte) string {
	t.Helper()

	reader, err := xz.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	return string(out)
}

const payload = "the quick brown fox jumps over the lazy dog\n"

// TestFiles: compress a file beside itself and remove it, keeping its mode
// and time; decompress it back; --keep keeps, --stdout streams.
func TestFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "data.txt")
	writeFile(t, file, payload)

	when := time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}

	mustRun(t, "xz", nil, file)

	if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("original still there: %v", err)
	}

	info, err := os.Stat(file + ".xz")
	if err != nil {
		t.Fatal(err)
	}

	// Windows has no permission bits to carry over.
	if (runtime.GOOS != "windows" && info.Mode().Perm() != 0o640) || !info.ModTime().Equal(when) {
		t.Errorf("mode %v, mtime %v", info.Mode(), info.ModTime())
	}

	if got := decode(t, []byte(readFile(t, file+".xz"))); got != payload {
		t.Errorf("decoded %q", got)
	}

	mustRun(t, "xz", nil, "-dk", file+".xz")

	if got := readFile(t, file); got != payload {
		t.Errorf("decompressed %q", got)
	}

	if _, err := os.Stat(file + ".xz"); err != nil {
		t.Errorf("--keep removed the input: %v", err)
	}

	if got := mustRun(t, "xz", nil, "-dc", file+".xz"); got != payload {
		t.Errorf("--stdout: %q", got)
	}

	// A second compression refuses to overwrite without --force, as a warning.
	status, _, stderr := run(t, "xz", nil, "-k", file)
	if status != filecodec.ExitWarning || !strings.Contains(stderr, "already exists") {
		t.Errorf("exit %d, stderr %q", status, stderr)
	}

	mustRun(t, "xz", nil, "-kf", file)
}

// TestStreams: no file, or "-", is standard input to standard output, in
// both directions and at every level; the aliases pick the direction.
func TestStreams(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"-0", "-6", "-9", "--fast", "--best", "-T0", "--threads=2", "-e", "-Fxz", "--format=auto", "-Ccrc64", "--check=crc64"} {
		compressed := mustRun(t, "xz", []byte(payload), level)
		if decode(t, []byte(compressed)) != payload {
			t.Errorf("%s: round trip", level)
		}

		if got := mustRun(t, "xz", []byte(compressed), "-d", "-"); got != payload {
			t.Errorf("%s: -d: %q", level, got)
		}

		if got := mustRun(t, "unxz", []byte(compressed)); got != payload {
			t.Errorf("%s: unxz: %q", level, got)
		}

		if got := mustRun(t, "/usr/bin/xzcat", []byte(compressed)); got != payload {
			t.Errorf("%s: xzcat: %q", level, got)
		}
	}
}

// TestRefusals: the usage errors, the suffix rules, bad data, and --test.
func TestRefusals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	writeFile(t, plain, payload)

	// The OS's words for a missing file.
	missing := "no such file"
	if runtime.GOOS == "windows" {
		missing = "cannot find the file"
	}

	for _, tc := range []struct {
		args   []string
		status int
		want   string
	}{
		{[]string{"--bogus"}, filecodec.ExitError, "unrecognized option '--bogus'"},
		{[]string{"--ver"}, filecodec.ExitError, "option '--ver' is ambiguous; possibilities: '--verbose' '--version'"},
		{
			[]string{"--memlimit-=1MiB"},
			filecodec.ExitError,
			"option '--memlimit-' is ambiguous; possibilities: '--memlimit-compress' '--memlimit-decompress'",
		},
		{[]string{"-y"}, filecodec.ExitError, "invalid option -- 'y'"},
		{[]string{"-T"}, filecodec.ExitError, "requires an argument"},
		{[]string{"-Tx"}, filecodec.ExitError, "invalid thread count"},
		{[]string{"-Fraw"}, filecodec.ExitError, "raw: Unsupported file format type"},
		{[]string{"--format=lzma"}, filecodec.ExitError, "lzma: Unsupported file format type"},
		{[]string{"--check=crc32"}, filecodec.ExitError, "crc32: Unsupported integrity check type"},
		{[]string{"-d", plain}, filecodec.ExitWarning, "unknown suffix -- ignored"},
		{[]string{"-d", filepath.Join(dir, "missing.xz")}, filecodec.ExitError, "missing.xz"},
		{[]string{"-dc", plain + ".xz"}, filecodec.ExitError, missing},
		{[]string{"-d", dir}, filecodec.ExitWarning, "not a regular file -- ignored"},
	} {
		status, _, stderr := run(t, "xz", nil, tc.args...)
		if status != tc.status || !strings.Contains(strings.ToLower(stderr), strings.ToLower(tc.want)) {
			t.Errorf("xz %v: exit %d, stderr %q, want %d %q", tc.args, status, stderr, tc.status, tc.want)
		}
	}

	status, _, stderr := run(t, "xz", []byte("this is not xz"), "-d")
	if status != filecodec.ExitError || !strings.Contains(stderr, "File format not recognized") {
		t.Errorf("bad data: exit %d, stderr %q", status, stderr)
	}

	compressed := mustRun(t, "xz", []byte(payload))

	status, _, stderr = run(t, "xz", []byte(compressed[:len(compressed)-4]), "-t")
	if status != filecodec.ExitError || stderr == "" {
		t.Errorf("truncated --test: exit %d, stderr %q", status, stderr)
	}

	if got := mustRun(t, "xz", []byte(compressed), "-t"); got != "" {
		t.Errorf("--test wrote %q", got)
	}

	// The abbreviated forms, any unambiguous prefix, as getopt_long takes
	// them for xz; a whole name wins over the longer names it prefixes.
	for _, flag := range []string{"--help", "-V", "--he", "--vers", "--memlimit=64MiB"} {
		if got := mustRun(t, "xz", nil, flag); got == "" {
			t.Errorf("%s printed nothing", flag)
		}
	}
}

// TestInterop: streams exchanged with the xz on PATH that is not ours, when
// there is one.
func TestInterop(t *testing.T) {
	t.Parallel()

	system := reference.Find(t, "xz", "-V")

	ours := mustRun(t, "xz", []byte(payload))

	cmd := exec.Command(system, "-dc")
	cmd.Stdin = strings.NewReader(ours)

	theirs, err := cmd.Output()
	if err != nil || string(theirs) != payload {
		t.Fatalf("%s -dc: %q %v", system, theirs, err)
	}

	cmd = exec.Command(system, "-c")
	cmd.Stdin = strings.NewReader(payload)

	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	if got := mustRun(t, "xz", compressed, "-d"); got != payload {
		t.Errorf("their stream: %q", got)
	}
}

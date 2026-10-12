package gzipcmd_test

// The gzip command from the outside: files compressed beside themselves and
// removed, decompressed back by suffix, the original's name and time in the
// header unless -n, standard streams when no file is named, gzip's
// refusals, and streams the gzip every host has can read.

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kgzip "github.com/klauspost/compress/gzip"

	"github.com/forkcloser/compress/internal/filecodec"
	"github.com/forkcloser/compress/internal/gzipcmd"
)

func run(t *testing.T, name string, stdin []byte, args ...string) (status int, stdout, stderr string) {
	t.Helper()

	var out, diag bytes.Buffer

	status = gzipcmd.Run(name, args, bytes.NewReader(stdin), &out, &diag, "test")

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

// decode reads a gzip stream through the library, returning the payload and
// the header, as a check independent of the command.
func decode(t *testing.T, data []byte) (string, kgzip.Header) {
	t.Helper()

	reader, err := kgzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	return string(out), reader.Header
}

const payload = "the quick brown fox jumps over the lazy dog\n"

// TestFiles: compress a file beside itself and remove it, the header
// carrying its name and time; decompress it back by suffix; -n leaves the
// header empty; .tgz comes back as .tar.
func TestFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "data.txt")
	writeFile(t, file, payload)

	when := time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}

	mustRun(t, "gzip", nil, file)

	if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("original still there: %v", err)
	}

	got, header := decode(t, []byte(readFile(t, file+".gz")))
	if got != payload || header.Name != "data.txt" || !header.ModTime.Equal(when) {
		t.Errorf("decoded %q, header %+v", got, header)
	}

	mustRun(t, "gunzip", nil, file+".gz")

	if got := readFile(t, file); got != payload {
		t.Errorf("decompressed %q", got)
	}

	if _, err := os.Stat(file + ".gz"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("gunzip kept the input: %v", err)
	}

	mustRun(t, "gzip", nil, "-nk", file)

	// The header's time field is 0: the reader reports that as the epoch.
	_, header = decode(t, []byte(readFile(t, file+".gz")))
	if header.Name != "" || header.ModTime.Unix() > 0 {
		t.Errorf("-n header %+v", header)
	}

	tarball := filepath.Join(dir, "pkg.tgz")
	writeFile(t, tarball, mustRun(t, "gzip", []byte("tar bytes")))
	mustRun(t, "gzip", nil, "-d", tarball)

	if got := readFile(t, filepath.Join(dir, "pkg.tar")); got != "tar bytes" {
		t.Errorf("pkg.tar: %q", got)
	}
}

// TestStreams: no file, or "-", is standard input to standard output, in
// both directions and at every level; the aliases pick the direction.
func TestStreams(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"-1", "-6", "-9", "--fast", "--best"} {
		compressed := mustRun(t, "gzip", []byte(payload), level)

		if got, header := decode(t, []byte(compressed)); got != payload || header.Name != "" {
			t.Errorf("%s: round trip %q, header %+v", level, got, header)
		}

		if got := mustRun(t, "gzip", []byte(compressed), "-d", "-"); got != payload {
			t.Errorf("%s: -d: %q", level, got)
		}

		if got := mustRun(t, "gunzip", []byte(compressed)); got != payload {
			t.Errorf("%s: gunzip: %q", level, got)
		}

		if got := mustRun(t, "zcat", []byte(compressed)); got != payload {
			t.Errorf("%s: zcat: %q", level, got)
		}
	}
}

// TestRefusals: the usage errors, the suffix rules, bad data, and --test.
func TestRefusals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	writeFile(t, plain, payload)

	for _, tc := range []struct {
		args   []string
		status int
		want   string
	}{
		{[]string{"--bogus"}, filecodec.ExitError, "unrecognized option '--bogus'"},
		{[]string{"-0"}, filecodec.ExitError, "compression level 0"},
		{[]string{"-d", plain}, filecodec.ExitWarning, "unknown suffix -- ignored"},
		{[]string{"-d", filepath.Join(dir, "missing.gz")}, filecodec.ExitError, "missing.gz"},
		{[]string{dir}, filecodec.ExitWarning, "not a regular file -- ignored"},
	} {
		status, _, stderr := run(t, "gzip", nil, tc.args...)
		if status != tc.status || !strings.Contains(stderr, tc.want) {
			t.Errorf("gzip %v: exit %d, stderr %q, want %d %q", tc.args, status, stderr, tc.status, tc.want)
		}
	}

	status, _, stderr := run(t, "gzip", []byte("this is not gzip"), "-d")
	if status != filecodec.ExitError || !strings.Contains(stderr, "not in gzip format") {
		t.Errorf("bad data: exit %d, stderr %q", status, stderr)
	}

	compressed := mustRun(t, "gzip", []byte(payload))

	status, _, stderr = run(t, "gzip", []byte(compressed[:len(compressed)-8]), "-t")
	if status != filecodec.ExitError || stderr == "" {
		t.Errorf("trailer cut, --test: exit %d, stderr %q", status, stderr)
	}

	if got := mustRun(t, "gzip", []byte(compressed), "-t"); got != "" {
		t.Errorf("--test wrote %q", got)
	}

	writeFile(t, plain+".gz", "existing")

	status, _, stderr = run(t, "gzip", nil, "-k", plain)
	if status != filecodec.ExitWarning || !strings.Contains(stderr, "already exists; not overwritten") {
		t.Errorf("exit %d, stderr %q", status, stderr)
	}

	if got := readFile(t, plain+".gz"); got != "existing" {
		t.Errorf("overwritten without --force: %q", got)
	}

	for _, flag := range []string{"--help", "-V"} {
		if got := mustRun(t, "gzip", nil, flag); got == "" {
			t.Errorf("%s printed nothing", flag)
		}
	}
}

// TestInterop: streams exchanged with the gzip on PATH, when there is one.
func TestInterop(t *testing.T) {
	t.Parallel()

	system, err := exec.LookPath("gzip")
	if err != nil {
		t.Skip("no gzip on PATH")
	}

	ours := mustRun(t, "gzip", []byte(payload))

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

	if got := mustRun(t, "gzip", compressed, "-d"); got != payload {
		t.Errorf("their stream: %q", got)
	}
}

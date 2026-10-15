package zstdcmd_test

// The zstd command from the outside: files compressed beside themselves and
// kept, removed only under --rm, decompressed back by suffix, standard
// streams when no file is named, zstd's refusals, and streams the zstd on
// PATH can read.

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/forkcloser/compress/internal/filecodec"
	"github.com/forkcloser/compress/internal/reference"
	"github.com/forkcloser/compress/internal/zstdcmd"
)

func run(t *testing.T, name string, stdin []byte, args ...string) (status int, stdout, stderr string) {
	t.Helper()

	var out, diag bytes.Buffer

	status = zstdcmd.Run(name, args, bytes.NewReader(stdin), &out, &diag, "test")

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

// decode reads a zstd stream through the library, as a check independent of
// the command.
func decode(t *testing.T, data []byte) string {
	t.Helper()

	reader, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	defer reader.Close()

	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	return string(out)
}

const payload = "the quick brown fox jumps over the lazy dog\n"

// TestFiles: compress a file beside itself and keep it, the result taking
// its mode and time; --rm removes; decompress it back; --stdout streams.
func TestFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "data.txt")
	writeFile(t, file, payload)

	when := time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}

	mustRun(t, "zstd", nil, file)

	if _, err := os.Stat(file); err != nil {
		t.Errorf("zstd removed its input: %v", err)
	}

	info, err := os.Stat(file + ".zst")
	if err != nil {
		t.Fatal(err)
	}

	// Windows has no permission bits to carry over.
	if (runtime.GOOS != "windows" && info.Mode().Perm() != 0o640) || !info.ModTime().Equal(when) {
		t.Errorf("mode %v, mtime %v", info.Mode(), info.ModTime())
	}

	if got := decode(t, []byte(readFile(t, file+".zst"))); got != payload {
		t.Errorf("decoded %q", got)
	}

	// A second compression refuses to overwrite without --force: for zstd an
	// error, it has no warning status.
	status, _, stderr := run(t, "zstd", nil, file)
	if status != filecodec.ExitError || !strings.Contains(stderr, "already exists") {
		t.Errorf("exit %d, stderr %q", status, stderr)
	}

	mustRun(t, "zstd", nil, "-f", "--rm", file)

	if _, err := os.Stat(file); err == nil {
		t.Error("--rm kept the input")
	}

	mustRun(t, "zstd", nil, "-d", file+".zst")

	if got := readFile(t, file); got != payload {
		t.Errorf("decompressed %q", got)
	}

	if _, err := os.Stat(file + ".zst"); err != nil {
		t.Errorf("-d removed its input: %v", err)
	}

	if got := mustRun(t, "zstd", nil, "-dc", file+".zst"); got != payload {
		t.Errorf("--stdout: %q", got)
	}
}

// TestStreams: no file, or "-", is standard input to standard output, in
// both directions and at every level; the aliases pick the direction.
func TestStreams(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"-1", "-3", "-19", "--fast", "--best", "-T0", "--threads=2", "--ultra", "-q19", "--format=zstd"} {
		compressed := mustRun(t, "zstd", []byte(payload), level)
		if decode(t, []byte(compressed)) != payload {
			t.Errorf("%s: round trip", level)
		}

		if got := mustRun(t, "zstd", []byte(compressed), "-d", "-"); got != payload {
			t.Errorf("%s: -d: %q", level, got)
		}

		if got := mustRun(t, "unzstd", []byte(compressed)); got != payload {
			t.Errorf("%s: unzstd: %q", level, got)
		}

		if got := mustRun(t, "/usr/bin/zstdcat", []byte(compressed)); got != payload {
			t.Errorf("%s: zstdcat: %q", level, got)
		}

		if got := mustRun(t, "zstdmt", []byte(compressed), "-d"); got != payload {
			t.Errorf("%s: zstdmt -d: %q", level, got)
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
		args []string
		want string
	}{
		{[]string{"--bogus"}, "unrecognized option '--bogus'"},
		{[]string{"-y"}, "invalid option -- 'y'"},
		{[]string{"-T"}, "requires an argument"},
		{[]string{"-Tx"}, "invalid thread count"},
		{[]string{"--format=gzip"}, "gzip: Unsupported file format type"},
		{[]string{"-0"}, "compression level 0 is not between 1 and 19"},
		{[]string{"-20"}, "compression level 20 is not between 1 and 19"},
		{[]string{"-99999999999999999999"}, "compression level 99999999999999999999 is not between"},
		{[]string{"-d", plain}, "unknown suffix -- ignored"},
		{[]string{"-d", filepath.Join(dir, "missing.zst")}, "missing.zst"},
		{[]string{"-d", dir}, "not a regular file -- ignored"},
	} {
		status, _, stderr := run(t, "zstd", nil, tc.args...)
		if status != filecodec.ExitError || !strings.Contains(strings.ToLower(stderr), strings.ToLower(tc.want)) {
			t.Errorf("zstd %v: exit %d, stderr %q, want %q", tc.args, status, stderr, tc.want)
		}
	}

	status, _, stderr := run(t, "zstd", []byte("this is not zstd"), "-d")
	if status != filecodec.ExitError || stderr == "" {
		t.Errorf("bad data: exit %d, stderr %q", status, stderr)
	}

	compressed := mustRun(t, "zstd", []byte(payload))

	status, _, stderr = run(t, "zstd", []byte(compressed[:len(compressed)-4]), "-t")
	if status != filecodec.ExitError || stderr == "" {
		t.Errorf("truncated --test: exit %d, stderr %q", status, stderr)
	}

	if got := mustRun(t, "zstd", []byte(compressed), "-t"); got != "" {
		t.Errorf("--test wrote %q", got)
	}

	for _, flag := range []string{"--help", "-V"} {
		if got := mustRun(t, "zstd", nil, flag); got == "" {
			t.Errorf("%s printed nothing", flag)
		}
	}
}

// TestInterop: streams exchanged with the zstd on PATH that is not ours, when
// there is one.
func TestInterop(t *testing.T) {
	t.Parallel()

	system := reference.Find(t, "zstd", "-V")

	ours := mustRun(t, "zstd", []byte(payload), "-19")

	cmd := exec.Command(system, "-dc")
	cmd.Stdin = strings.NewReader(ours)

	theirs, err := cmd.Output()
	if err != nil || string(theirs) != payload {
		t.Fatalf("%s -dc: %q %v", system, theirs, err)
	}

	cmd = exec.Command(system, "-c", "-19")
	cmd.Stdin = strings.NewReader(payload)

	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	if got := mustRun(t, "zstd", compressed, "-d"); got != payload {
		t.Errorf("their stream: %q", got)
	}
}

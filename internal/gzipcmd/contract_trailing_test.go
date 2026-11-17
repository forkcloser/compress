package gzipcmd_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractTrailingGarbageIsAWarning: bytes after the last member that
// do not begin another are not the stream's. gzip writes the data, says
// "trailing garbage ignored" and exits 2; --quiet takes the line and
// leaves the status. To a file, the counterpart stands and the input is
// removed. The zeros a tape block is padded with are such bytes. Input
// that begins a member, by its first byte, and is not one is still an
// error.
func TestContractTrailingGarbageIsAWarning(t *testing.T) {
	t.Parallel()

	compressed := mustRun(t, "gzip", []byte(payload))

	const words = "gzip: (stdin): trailing garbage ignored\n"

	for name, tail := range map[string]string{
		"garbage":      "garbage!",
		"one byte":     "x",
		"zero padding": strings.Repeat("\x00", 512),
	} {
		for members, want := range map[string]string{compressed: payload, compressed + compressed: payload + payload} {
			status, stdout, stderr := run(t, "gzip", []byte(members+tail), "-dc")
			if status != filecodec.ExitWarning || stdout != want || stderr != words {
				t.Errorf("%s: exit %d, stdout %q, stderr %q", name, status, stdout, stderr)
			}
		}

		status, stdout, stderr := run(t, "gzip", []byte(compressed+tail), "-dcq")
		if status != filecodec.ExitWarning || stdout != payload || stderr != "" {
			t.Errorf("%s, --quiet: exit %d, stdout %q, stderr %q", name, status, stdout, stderr)
		}

		status, stdout, stderr = run(t, "gzip", []byte(compressed+tail), "-t")
		if status != filecodec.ExitWarning || stdout != "" || stderr != words {
			t.Errorf("%s, --test: exit %d, stdout %q, stderr %q", name, status, stdout, stderr)
		}
	}

	file := filepath.Join(t.TempDir(), "data.gz")
	writeFile(t, file, compressed+"garbage!")

	status, _, stderr := run(t, "gzip", nil, "-d", file)
	if status != filecodec.ExitWarning || !strings.HasSuffix(stderr, "data.gz: trailing garbage ignored\n") {
		t.Errorf("to a file: exit %d, stderr %q", status, stderr)
	}

	if got := readFile(t, strings.TrimSuffix(file, ".gz")); got != payload {
		t.Errorf("to a file: the counterpart holds %q", got)
	}

	if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("to a file: the input stayed: %v", err)
	}

	for name, tail := range map[string]string{
		"the first byte of a member alone": "\x1f",
		"a member cut in its header":       "\x1f\x8b\x08",
		"a member's first byte, then not":  "\x1fgarbage!",
	} {
		status, stdout, stderr := run(t, "gzip", []byte(compressed+tail), "-dc")
		if status != filecodec.ExitError || stdout != payload || stderr == "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", name, status, stdout, stderr)
		}
	}
}

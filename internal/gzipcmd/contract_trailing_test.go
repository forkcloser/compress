package gzipcmd_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/filecodec"
)

// TestContractTrailingGarbageIsAWarning: bytes after the last member that
// do not begin another are not the stream's. gzip writes the data, says
// "trailing garbage ignored" and exits 2; --quiet takes the line and
// leaves the status. To a file, the counterpart stands and the input is
// removed. Zeros to the end are not such bytes: a tape block is padded
// with them, and gzip passes over them in silence; whatever comes after
// zeros is garbage, a member too. Input that begins a member, by its first
// byte, and is not one is still an error.
func TestContractTrailingGarbageIsAWarning(t *testing.T) {
	t.Parallel()

	compressed := mustRun(t, "gzip", []byte(payload))

	const words = "gzip: (stdin): trailing garbage ignored\n"

	for name, zeros := range map[string]string{"zero padding": strings.Repeat("\x00", 512), "one zero": "\x00"} {
		for _, args := range []string{"-dc", "-t"} {
			status, stdout, stderr := run(t, "gzip", []byte(compressed+zeros), args)
			if status != filecodec.ExitOK || (args == "-dc" && stdout != payload) || stderr != "" {
				t.Errorf("%s, %s: exit %d, stdout %q, stderr %q", name, args, status, stdout, stderr)
			}
		}
	}

	// After zeros, a member is garbage like anything else: padding ends
	// the members.
	for name, tail := range map[string]string{
		"garbage":              "garbage!",
		"one byte":             "x",
		"zeros, then garbage":  "\x00\x00x",
		"zeros, then a member": "\x00\x00" + compressed,
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

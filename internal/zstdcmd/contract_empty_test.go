package zstdcmd_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/filecodec"
)

// TestContractEmptyInputIsNotAStream: no bytes at all are not a zstd
// stream. --test, --decompress and --decompress --stdout fail on an empty
// input with "unexpected end of file", status 1, and write nothing, no
// counterpart either: a download cut to nothing does not test clean.
func TestContractEmptyInputIsNotAStream(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "empty.zst")
	writeFile(t, file, "")

	for _, flag := range []string{"-t", "-d", "-dc"} {
		for input, args := range map[string][]string{"(stdin)": {flag}, "empty.zst": {flag, file}} {
			status, stdout, stderr := run(t, "zstd", []byte{}, args...)
			if status != filecodec.ExitError || stdout != "" ||
				!strings.HasSuffix(stderr, input+": unexpected end of file\n") {
				t.Errorf("zstd %s %s: exit %d, stdout %q, stderr %q", flag, input, status, stdout, stderr)
			}
		}
	}

	if _, err := os.Stat(strings.TrimSuffix(file, ".zst")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a counterpart was written: %v", err)
	}

	// An empty file compressed is a stream, and not an empty one.
	if got := mustRun(t, "zstd", []byte(mustRun(t, "zstd", []byte{})), "-d"); got != "" {
		t.Errorf("the empty file's stream decodes to %q", got)
	}
}

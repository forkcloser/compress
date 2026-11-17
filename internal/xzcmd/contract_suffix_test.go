package xzcmd_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/filecodec"
)

// TestContractSuffixedInputIsSkipped: xz does not compress a file that
// already has one of its suffixes: a warning, status 2, and nothing
// written. --force compresses it.
func TestContractSuffixedInputIsSkipped(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"data.xz", "data.txz"} {
		file := filepath.Join(t.TempDir(), name)
		writeFile(t, file, payload)

		status, _, stderr := run(t, "xz", nil, "-k", file)
		if status != filecodec.ExitWarning || !strings.Contains(stderr, "suffix -- unchanged") {
			t.Errorf("%s: exit %d, stderr %q", name, status, stderr)
		}

		if _, err := os.Stat(file + ".xz"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s.xz was written: %v", name, err)
		}

		mustRun(t, "xz", nil, "-kf", file)

		if _, err := os.Stat(file + ".xz"); err != nil {
			t.Errorf("--force: %v", err)
		}
	}
}

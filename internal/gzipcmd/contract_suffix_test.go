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

// TestContractSuffixedInputIsSkipped: gzip does not compress a file that
// already has one of its suffixes: it says so, writes nothing, and the
// status is not touched, as GNU gzip's is not. --force compresses it.
func TestContractSuffixedInputIsSkipped(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"data.gz", "data.tgz"} {
		file := filepath.Join(t.TempDir(), name)
		writeFile(t, file, payload)

		status, _, stderr := run(t, "gzip", nil, "-k", file)
		if status != filecodec.ExitOK || !strings.Contains(stderr, "suffix -- unchanged") {
			t.Errorf("%s: exit %d, stderr %q", name, status, stderr)
		}

		if _, err := os.Stat(file + ".gz"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s.gz was written: %v", name, err)
		}

		mustRun(t, "gzip", nil, "-kf", file)

		if _, err := os.Stat(file + ".gz"); err != nil {
			t.Errorf("--force: %v", err)
		}
	}
}

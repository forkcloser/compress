package zstdcmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractSymbolicLinkInputIsLeftAlone: zstd does not read an input
// that is a symbolic link, wherever the result goes: it skips it, status 1,
// and writes nothing. --force takes it. A file with other names is a file
// like any other.
func TestContractSymbolicLinkInputIsLeftAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	writeFile(t, data, payload)
	mustRun(t, "zstd", nil, data)

	link, other := filepath.Join(dir, "link"), filepath.Join(dir, "other.zst")

	for name, as := range map[string]string{"data": "link", "data.zst": "other.zst"} {
		if err := os.Symlink(name, filepath.Join(dir, as)); err != nil {
			t.Skipf("no symbolic link here: %v", err)
		}
	}

	for _, args := range [][]string{
		{link}, {"-c", link}, {"--rm", link}, {"-d", other}, {"-dc", other}, {"-t", other},
	} {
		status, stdout, stderr := run(t, "zstd", nil, args...)
		if status != filecodec.ExitError || stdout != "" || !strings.Contains(stderr, "is a symbolic link -- ignored") {
			t.Errorf("zstd %v: exit %d, stdout %q, stderr %q", args, status, stdout, stderr)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 4 {
		t.Errorf("the directory holds %v", entries)
	}

	if got := mustRun(t, "zstd", nil, "-dcf", other); got != payload {
		t.Errorf("--force --stdout: %q", got)
	}

	mustRun(t, "zstd", nil, "-f", link)

	if got := decode(t, []byte(readFile(t, link+".zst"))); got != payload {
		t.Errorf("--force: %q", got)
	}

	hard := filepath.Join(dir, "hard")
	if err := os.Link(data, hard); err != nil {
		t.Skipf("no hard link here: %v", err)
	}

	mustRun(t, "zstd", nil, hard)

	if got := decode(t, []byte(readFile(t, hard+".zst"))); got != payload {
		t.Errorf("a file with two names: %q", got)
	}
}

package xzcmd_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// linked is a directory holding data, a compressed copy of it, and a link
// of the given kind to each: link and other.xz.
func linked(t *testing.T, kind string) string {
	t.Helper()

	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	writeFile(t, data, payload)
	mustRun(t, "xz", nil, "-k", data)

	for name, as := range map[string]string{"data": "link", "data.xz": "other.xz"} {
		at := filepath.Join(dir, as)

		var err error
		if kind == "symbolic" {
			err = os.Symlink(name, at)
		} else {
			err = os.Link(filepath.Join(dir, name), at)
		}

		if err != nil {
			t.Skipf("no such link here: %v", err)
		}
	}

	return dir
}

// TestContractLinkedInputIsLeftAlone: xz does not compress, or decompress,
// and then remove an input that is a symbolic link or a file with other
// names: it skips it with a warning, status 2, and writes nothing. Where
// the input stays, under --keep, --stdout and --test, it is read as any
// file; --force takes it and removes that one name.
func TestContractLinkedInputIsLeftAlone(t *testing.T) {
	t.Parallel()

	for kind, words := range map[string]string{
		"symbolic": "is a symbolic link -- ignored",
		"hard":     "has other links -- unchanged",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			if kind == "hard" && runtime.GOOS == "windows" {
				t.Skip("a file's link count is not in what Stat answers there")
			}

			dir := linked(t, kind)
			link, other := filepath.Join(dir, "link"), filepath.Join(dir, "other.xz")

			for _, args := range [][]string{{link}, {"-d", other}} {
				status, _, stderr := run(t, "xz", nil, args...)
				if status != filecodec.ExitWarning || !strings.Contains(stderr, words) {
					t.Errorf("xz %v: exit %d, stderr %q", args, status, stderr)
				}
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}

			if len(entries) != 4 {
				t.Errorf("the directory holds %v", entries)
			}

			if got := mustRun(t, "xz", nil, "-dc", other); got != payload {
				t.Errorf("--stdout: %q", got)
			}

			mustRun(t, "xz", nil, "-t", other)

			if got := decode(t, []byte(mustRun(t, "xz", nil, "-c", link))); got != payload {
				t.Errorf("--stdout, compressing: %q", got)
			}

			keep := filepath.Join(dir, "kept")
			if err := os.Rename(link, keep); err != nil {
				t.Fatal(err)
			}

			mustRun(t, "xz", nil, "-k", keep)

			if got := decode(t, []byte(readFile(t, keep+".xz"))); got != payload {
				t.Errorf("--keep: %q", got)
			}

			// --force: the name given goes, the file stays under its other name.
			mustRun(t, "xz", nil, "-f", keep)

			if _, err := os.Lstat(keep); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("--force left the input: %v", err)
			}

			if got := readFile(t, filepath.Join(dir, "data")); got != payload {
				t.Errorf("the file behind the link: %q", got)
			}
		})
	}
}

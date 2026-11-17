package gzipcmd_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/filecodec"
)

// TestContractLinkedInputIsLeftAlone: gzip does not compress, or
// decompress, into a file an input that is a symbolic link or a file with
// other names, kept or not: it skips it with a warning, status 2, and
// writes nothing. --stdout and --test read it as any file; --force takes
// it and removes that one name.
func TestContractLinkedInputIsLeftAlone(t *testing.T) {
	t.Parallel()

	// GNU gzip fails to open a symbolic link, an error, and skips a file
	// with other names, a warning.
	for kind, words := range map[string]string{
		"symbolic": "Too many levels of symbolic links",
		"hard":     "has other links -- unchanged",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			if kind == "hard" && runtime.GOOS == "windows" {
				t.Skip("a file's link count is not in what Stat answers there")
			}

			dir := t.TempDir()
			data := filepath.Join(dir, "data")
			writeFile(t, data, payload)
			mustRun(t, "gzip", nil, "-k", data)

			link, other := filepath.Join(dir, "link"), filepath.Join(dir, "other.gz")

			for name, as := range map[string]string{"data": "link", "data.gz": "other.gz"} {
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

			for _, args := range [][]string{{link}, {"-k", link}, {"-d", other}, {"-dk", other}} {
				status, _, stderr := run(t, "gzip", nil, args...)

				want := filecodec.ExitWarning
				if kind == "symbolic" {
					want = filecodec.ExitError
				}

				if status != want || !strings.Contains(stderr, words) {
					t.Errorf("gzip %v: exit %d, stderr %q", args, status, stderr)
				}
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}

			if len(entries) != 4 {
				t.Errorf("the directory holds %v", entries)
			}

			if got := mustRun(t, "gzip", nil, "-dc", other); got != payload {
				t.Errorf("--stdout: %q", got)
			}

			mustRun(t, "gzip", nil, "-t", other)

			if got, _ := decode(t, []byte(mustRun(t, "gzip", nil, "-c", link))); got != payload {
				t.Errorf("--stdout, compressing: %q", got)
			}

			// --force: the name given goes, the file stays under its other name.
			mustRun(t, "gzip", nil, "-f", link)

			if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("--force left the input: %v", err)
			}

			if got := readFile(t, data); got != payload {
				t.Errorf("the file behind the link: %q", got)
			}
		})
	}
}

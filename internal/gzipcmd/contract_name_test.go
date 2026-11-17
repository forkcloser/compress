package gzipcmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/filecodec"
)

// TestContractNameRestoresTheOriginal: -N on decompression gives the
// counterpart the name and the time the stream records, not the ones the
// compressed file has now; without -N, or with -n after it, they come from
// the compressed file. A stream that records neither falls back on the
// file's. The recorded name is taken without its directories, an existing
// file of that name is not overwritten, and --stdout has no use for it.
func TestContractNameRestoresTheOriginal(t *testing.T) {
	t.Parallel()

	then := time.Date(2024, time.May, 6, 7, 8, 9, 0, time.UTC)
	later := time.Date(2025, time.June, 7, 8, 9, 10, 0, time.UTC)

	// renamed is a directory holding other.gz: orig.txt compressed with the
	// given options, then renamed and touched.
	renamed := func(t *testing.T, options ...string) (dir, packed string) {
		t.Helper()

		dir = t.TempDir()
		file := filepath.Join(dir, "orig.txt")
		writeFile(t, file, payload)

		if err := os.Chtimes(file, then, then); err != nil {
			t.Fatal(err)
		}

		mustRun(t, "gzip", nil, append(options, file)...)

		packed = filepath.Join(dir, "other.gz")
		if err := os.Rename(file+".gz", packed); err != nil {
			t.Fatal(err)
		}

		if err := os.Chtimes(packed, later, later); err != nil {
			t.Fatal(err)
		}

		return dir, packed
	}

	for _, tc := range []struct {
		clause   string
		compress []string
		args     []string
		name     string
		when     time.Time
	}{
		{"-N restores both", nil, []string{"-dN"}, "orig.txt", then},
		{"--name restores both", nil, []string{"-d", "--name"}, "orig.txt", then},
		{"without -N, the file's", nil, []string{"-d"}, "other", later},
		{"-n after -N, the file's", nil, []string{"-dNn"}, "other", later},
		{"-N after -n restores both", nil, []string{"-dnN"}, "orig.txt", then},
		{"nothing recorded, the file's", []string{"-n"}, []string{"-dN"}, "other", later},
	} {
		t.Run(tc.clause, func(t *testing.T) {
			t.Parallel()

			dir, packed := renamed(t, tc.compress...)
			mustRun(t, "gzip", nil, append(tc.args, packed)...)

			info, err := os.Stat(filepath.Join(dir, tc.name))
			if err != nil {
				entries, _ := os.ReadDir(dir)
				t.Fatalf("no %s: the directory holds %v", tc.name, entries)
			}

			if !info.ModTime().Equal(tc.when) {
				t.Errorf("time %v, want %v", info.ModTime(), tc.when)
			}

			if got := readFile(t, filepath.Join(dir, tc.name)); got != payload {
				t.Errorf("content %q", got)
			}
		})
	}

	t.Run("an existing file of the recorded name", func(t *testing.T) {
		t.Parallel()

		dir, packed := renamed(t)
		writeFile(t, filepath.Join(dir, "orig.txt"), "mine")

		status, _, stderr := run(t, "gzip", nil, "-dN", packed)
		if status != filecodec.ExitWarning || !strings.Contains(stderr, "orig.txt already exists") {
			t.Errorf("exit %d, stderr %q", status, stderr)
		}

		if got := readFile(t, filepath.Join(dir, "orig.txt")); got != "mine" {
			t.Errorf("overwritten: %q", got)
		}
	})

	t.Run("--stdout", func(t *testing.T) {
		t.Parallel()

		dir, packed := renamed(t)
		if got := mustRun(t, "gzip", nil, "-dcN", packed); got != payload {
			t.Errorf("stdout %q", got)
		}

		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
			t.Errorf("the directory holds %v (%v)", entries, err)
		}
	})

	t.Run("a recorded name with directories", func(t *testing.T) {
		t.Parallel()

		// The header of a stream compressed from "../../evil", then the
		// rest of an ordinary one: flags at byte 3, the name after byte 9.
		stream := mustRun(t, "gzip", []byte(payload))
		forged := stream[:3] + "\x08" + stream[4:10] + "../../evil\x00" + stream[10:]

		dir := filepath.Join(t.TempDir(), "a", "b")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		writeFile(t, filepath.Join(dir, "x.gz"), forged)
		mustRun(t, "gzip", nil, "-dN", filepath.Join(dir, "x.gz"))

		if got := readFile(t, filepath.Join(dir, "evil")); got != payload {
			t.Errorf("content %q", got)
		}
	})
}

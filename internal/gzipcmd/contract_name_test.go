package gzipcmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractNameRestoresTheOriginal: -N on decompression gives the
// counterpart the name and the time the stream records, not the ones the
// compressed file has now; without -N, or with -n after it, they come from
// the compressed file. A stream that records neither falls back on the
// file's. The recorded name is taken without its directories, an existing
// file of that name is not overwritten, and --stdout has no use for it.
// Under --force the name of the file the stream is in is refused, and that
// of the symbolic link it is read through is taken: the counterpart
// replaces the link.
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

	// A stream that records the name of the file it is in, as written and
	// in another case: with --force too, neither is taken for a place to
	// make room at. The second is the first's own name where the volume
	// folds case, and another file's where it does not.
	for _, recorded := range []string{"x.gz", "X.GZ"} {
		t.Run("the recorded name "+recorded+" for x.gz", func(t *testing.T) {
			t.Parallel()

			stream := mustRun(t, "gzip", []byte(payload))
			forged := stream[:3] + "\x08" + stream[4:10] + recorded + "\x00" + stream[10:]

			dir := t.TempDir()
			file := filepath.Join(dir, "x.gz")
			writeFile(t, file, forged)

			_, err := os.Lstat(filepath.Join(dir, recorded))
			own := err == nil

			// Without --force the name is taken like any other: there.
			for args, want := range map[string]struct {
				status int
				words  string
			}{
				"-dN":   {filecodec.ExitWarning, "already exists; not overwritten"},
				"-dNf":  {filecodec.ExitError, "the stream records the name of its own file"},
				"-dNfk": {filecodec.ExitError, "the stream records the name of its own file"},
			} {
				status, _, stderr := run(t, "gzip", nil, args, file)

				if !own {
					// Another name: the counterpart is made there.
					if status != filecodec.ExitOK || readFile(t, filepath.Join(dir, recorded)) != payload {
						t.Errorf("gzip %v: exit %d, stderr %q", args, status, stderr)
					}

					return
				}

				if status != want.status || !strings.Contains(stderr, want.words) {
					t.Errorf("gzip %v: exit %d, stderr %q", args, status, stderr)
				}

				if got := readFile(t, file); got != forged {
					t.Errorf("gzip %v: the input holds %d bytes, want its own %d", args, len(got), len(forged))
				}

				if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
					t.Errorf("gzip %v: the directory holds %v (%v)", args, entries, err)
				}
			}
		})
	}

	// A stream that records the name of the symbolic link it is read
	// through: --force makes room there, the counterpart takes the link's
	// name, and the file the link was to stays as it is.
	for _, args := range []string{"-dNf", "-dNfk"} {
		t.Run("the recorded name of the link, "+args, func(t *testing.T) {
			t.Parallel()

			stream := mustRun(t, "gzip", []byte(payload))
			forged := stream[:3] + "\x08" + stream[4:10] + "sym.gz\x00" + stream[10:]

			dir := t.TempDir()
			file, link := filepath.Join(dir, "real.gz"), filepath.Join(dir, "sym.gz")
			writeFile(t, file, forged)

			if err := os.Symlink("real.gz", link); err != nil {
				t.Skipf("no such link here: %v", err)
			}

			status, _, stderr := run(t, "gzip", nil, args, link)
			if status != filecodec.ExitOK || stderr != "" {
				t.Errorf("exit %d, stderr %q", status, stderr)
			}

			if info, err := os.Lstat(link); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("the link's name holds %v (%v), want a file", info, err)
			}

			if got := readFile(t, link); got != payload {
				t.Errorf("the counterpart holds %q", got)
			}

			if got := readFile(t, file); got != forged {
				t.Errorf("the file the link was to holds %d bytes, want its own %d", len(got), len(forged))
			}

			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 2 {
				t.Errorf("the directory holds %v (%v)", entries, err)
			}
		})
	}

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

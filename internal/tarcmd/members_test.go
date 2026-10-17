package tarcmd_test

// Members named for --extract and --list, on the command line or in a -T
// file, select what is taken: an entry by its archive name, a directory
// with everything under it, nothing else; a name no entry carries is
// reported and the status is 2, as GNU tar's.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/tarcmd"
)

// memberArchive is the tree archived with its names at the top level.
func memberArchive(t *testing.T) string {
	t.Helper()

	src := tree(t)
	archive := filepath.Join(t.TempDir(), "a.tar")

	mustRun(t, nil, "-cf", archive, "-C", src, "hello.txt", "sub", "link")

	return archive
}

// present fails unless exactly the paths in want exist under dest, among
// those in want and absent.
func present(t *testing.T, dest string, want, absent []string) {
	t.Helper()

	for _, p := range want {
		if _, err := os.Lstat(filepath.Join(dest, p)); err != nil {
			t.Errorf("%s: %v, want it extracted", p, err)
		}
	}

	for _, p := range absent {
		if _, err := os.Lstat(filepath.Join(dest, p)); err == nil {
			t.Errorf("%s: extracted, want it skipped", p)
		}
	}
}

func TestExtractMembers(t *testing.T) {
	t.Parallel()

	archive := memberArchive(t)

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		dest := t.TempDir()
		mustRun(t, nil, "-xf", archive, "-C", dest, "sub/run.sh")
		present(t, dest, []string{"sub/run.sh"}, []string{"hello.txt", "link", "sub/deep"})

		if got := readFile(t, filepath.Join(dest, "sub", "run.sh")); got != "#!/bin/sh\n" {
			t.Errorf("sub/run.sh = %q", got)
		}
	})

	t.Run("directory with a trailing slash", func(t *testing.T) {
		t.Parallel()

		dest := t.TempDir()
		mustRun(t, nil, "-xf", archive, "-C", dest, "sub/")
		present(t, dest, []string{"sub/run.sh", "sub/deep/data.bin"}, []string{"hello.txt", "link"})
	})

	t.Run("list under strip-components", func(t *testing.T) {
		t.Parallel()

		dest := t.TempDir()
		list := filepath.Join(t.TempDir(), "members")
		writeFile(t, list, "sub/deep/data.bin\nlink\n", 0o644)

		mustRun(t, nil, "-xf", archive, "-C", dest, "--strip-components=1", "-T", list)
		present(t, dest, []string{"deep/data.bin"}, []string{"sub", "hello.txt", "link", "run.sh"})

		if got := readFile(t, filepath.Join(dest, "deep", "data.bin")); len(got) != 70000 {
			t.Errorf("deep/data.bin: %d bytes, want 70000", len(got))
		}
	})

	t.Run("exclude wins", func(t *testing.T) {
		t.Parallel()

		dest := t.TempDir()
		mustRun(t, nil, "-xf", archive, "-C", dest, "--exclude=run.sh", "sub")
		present(t, dest, []string{"sub/deep/data.bin"}, []string{"sub/run.sh", "hello.txt"})

		// A member both named and excluded is found, then skipped: no
		// "Not found in archive", status 0, as GNU tar has it.
		dest = t.TempDir()
		mustRun(t, nil, "-xf", archive, "-C", dest, "--exclude=run.sh", "sub/run.sh")
		present(t, dest, nil, []string{"sub"})
	})

	t.Run("missing member", func(t *testing.T) {
		t.Parallel()

		dest := t.TempDir()

		status, _, stderr := run(t, nil, "-xf", archive, "-C", dest, "hello.txt", "nope")
		if status != tarcmd.ExitError {
			t.Fatalf("exit %d, want %d\n%s", status, tarcmd.ExitError, stderr)
		}

		for _, want := range []string{"tar: nope: Not found in archive", "Exiting with failure status"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr %q lacks %q", stderr, want)
			}
		}

		present(t, dest, []string{"hello.txt"}, []string{"sub", "link"})
	})
}

func TestListMembers(t *testing.T) {
	t.Parallel()

	archive := memberArchive(t)

	stdout, _ := mustRun(t, nil, "-tf", archive, "sub/deep", "hello.txt")

	for line := range strings.SplitSeq(strings.TrimSuffix(stdout, "\n"), "\n") {
		if !strings.HasPrefix(line, "sub/deep") && line != "hello.txt" {
			t.Errorf("listed %q, outside the members", line)
		}
	}

	for _, want := range []string{"sub/deep/data.bin", "hello.txt"} {
		if !strings.Contains(stdout, want+"\n") {
			t.Errorf("listing lacks %q:\n%s", want, stdout)
		}
	}

	status, stdout, stderr := run(t, nil, "-tf", archive, "nope")
	if status != tarcmd.ExitError || stdout != "" || !strings.Contains(stderr, "tar: nope: Not found in archive") {
		t.Errorf("listing a missing member: exit %d, stdout %q, stderr %q", status, stdout, stderr)
	}
}

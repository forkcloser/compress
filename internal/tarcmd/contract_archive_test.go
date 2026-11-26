package tarcmd_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/reference"
	"github.com/forkcloser/go-compress/internal/tarcmd"
)

const notDumped = ": archive cannot contain itself; not dumped\n"

// TestContractArchiveIsNoMemberOfItself: an archive written inside the tree
// it archives is left out of itself, said on standard error, and the run
// succeeds, as GNU tar's does. The archive is known by what file it is, not
// by its name: through -f, through a redirected standard output, through a
// compressor, and under another name that is a hard link to it.
func TestContractArchiveIsNoMemberOfItself(t *testing.T) {
	t.Parallel()

	want := func(t *testing.T, archive []byte, kept ...string) {
		t.Helper()

		got := names(t, archive)
		for _, name := range kept {
			if !slices.Contains(got, name) {
				t.Errorf("entries %q lack %q", got, name)
			}
		}

		for _, name := range got {
			if strings.Contains(name, "out.") || strings.Contains(name, "alias") {
				t.Errorf("entries %q hold the archive as %q", got, name)
			}
		}
	}

	t.Run("the -f file", func(t *testing.T) {
		t.Parallel()

		src := tree(t)
		archive := filepath.Join(src, "sub", "out.tar")

		_, stderr := mustRun(t, nil, "-cf", archive, "-C", src, ".")
		if stderr != "tar: ./sub/out.tar"+notDumped {
			t.Errorf("stderr %q", stderr)
		}

		want(t, []byte(readFile(t, archive)), "./hello.txt", "./sub/run.sh")
	})

	t.Run("a compressed -f file", func(t *testing.T) {
		t.Parallel()

		src := tree(t)
		archive := filepath.Join(src, "out.tar.gz")

		_, stderr := mustRun(t, nil, "-czf", archive, "-C", src, ".")
		if stderr != "tar: ./out.tar.gz"+notDumped {
			t.Errorf("stderr %q", stderr)
		}

		listed, _ := mustRun(t, nil, "-tzf", archive)
		if got := strings.Fields(listed); !slices.Contains(got, "./hello.txt") || slices.Contains(got, "./out.tar.gz") {
			t.Errorf("entries %q", got)
		}
	})

	t.Run("standard output", func(t *testing.T) {
		t.Parallel()

		src := tree(t)
		archive := filepath.Join(src, "out.tar")

		file, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}

		var stderr bytes.Buffer

		status := tarcmd.Run([]string{"-cf", "-", "-C", src, "."}, nil, file, &stderr, "test")
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}

		if status != tarcmd.ExitOK || stderr.String() != "tar: ./out.tar"+notDumped {
			t.Errorf("exit %d, stderr %q", status, stderr.String())
		}

		want(t, []byte(readFile(t, archive)), "./hello.txt")
	})

	t.Run("a hard link to it", func(t *testing.T) {
		t.Parallel()

		src := tree(t)
		archive := filepath.Join(t.TempDir(), "out.tar")
		writeFile(t, archive, "", 0o644)

		if err := os.Link(archive, filepath.Join(src, "alias")); err != nil {
			t.Skipf("no hard link here: %v", err)
		}

		_, stderr := mustRun(t, nil, "-cf", archive, "-C", src, ".")
		if stderr != "tar: ./alias"+notDumped {
			t.Errorf("stderr %q", stderr)
		}

		want(t, []byte(readFile(t, archive)), "./hello.txt")
	})

	t.Run("a named member", func(t *testing.T) {
		t.Parallel()

		src := tree(t)
		archive := filepath.Join(src, "out.tar")

		_, stderr := mustRun(t, nil, "-cf", archive, "-C", src, "hello.txt", "out.tar")
		if stderr != "tar: out.tar"+notDumped {
			t.Errorf("stderr %q", stderr)
		}

		want(t, []byte(readFile(t, archive)), "hello.txt")
	})

	t.Run("as GNU tar says it", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("the GNU tar on a Windows runner's PATH is Git for Windows', which fails on these paths")
		}

		system := reference.Find(t, "tar", "--version")

		version, err := exec.CommandContext(t.Context(), system, "--version").Output()
		if err != nil || !strings.Contains(string(version), "GNU tar") {
			t.Skip("the tar on PATH is not GNU tar")
		}

		src := tree(t)
		archive := filepath.Join(src, "out.tar")

		theirs, err := exec.CommandContext(t.Context(), system, "-cf", archive, "-C", src, ".").CombinedOutput()
		if err != nil {
			t.Fatalf("GNU tar: %v\n%s", err, theirs)
		}

		want(t, []byte(readFile(t, archive)), "./hello.txt")

		// GNU tar says its own name as it was run; this one says "tar".
		said := strings.Replace(string(theirs), system+": ", "tar: ", 1)

		_, ours := mustRun(t, nil, "-cf", archive, "-C", src, ".")
		if ours != said {
			t.Errorf("ours says %q, GNU tar %q", ours, said)
		}
	})
}

// TestContractArchiveNamedFromTheWorkingDirectory: the archive is known as
// itself when -f names it relative to the working directory.
func TestContractArchiveNamedFromTheWorkingDirectory(t *testing.T) {
	src := tree(t)
	t.Chdir(filepath.Dir(src))

	_, stderr := mustRun(t, nil, "-cf", "src/out.tar", "src")
	if stderr != "tar: src/out.tar"+notDumped {
		t.Errorf("stderr %q", stderr)
	}

	got := names(t, []byte(readFile(t, filepath.Join(src, "out.tar"))))
	if slices.Contains(got, "src/out.tar") || !slices.Contains(got, "src/hello.txt") {
		t.Errorf("entries %q", got)
	}
}

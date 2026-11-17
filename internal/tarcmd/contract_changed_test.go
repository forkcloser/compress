package tarcmd_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/reference"
	"github.com/forkcloser/compress/internal/tarcmd"
)

// changes are what can happen to a tree while it is archived: to the large
// file whose content is coming out, or to the directory around it.
func changes() map[string]func(dir string) error {
	big := func(dir string) string { return filepath.Join(dir, "big") }

	return map[string]func(dir string) error{
		"left alone":      func(string) error { return nil },
		"the file shrank": func(dir string) error { return os.Truncate(big(dir), 10) },
		"the file grew": func(dir string) error {
			f, err := os.OpenFile(big(dir), os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}

			if _, err := f.Write(make([]byte, 1<<20)); err != nil {
				return err
			}

			return f.Close()
		},
		"the file was touched": func(dir string) error {
			later := time.Now().Add(time.Hour)

			return os.Chtimes(big(dir), later, later)
		},
		"a name came": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "late"), []byte("x"), 0o644)
		},
	}
}

// changing builds root/tree with a small file and a large one, and returns
// root: the archive goes to a pipe, and once the large file's content is
// coming out the small one before it is archived and the tree is changed,
// before the pipe is drained.
func changing(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "tree")

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, "a"), "a", 0o644)

	if err := os.WriteFile(filepath.Join(dir, "big"), make([]byte, 4<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	return root
}

// into reads the archive from read past the directory's entry and the small
// file's, into the large file's content, applies change, and drains it.
func into(t *testing.T, read io.Reader, change func() error) {
	t.Helper()

	if _, err := io.ReadFull(read, make([]byte, 4*512+4096)); err != nil {
		t.Fatal(err)
	}

	if err := change(); err != nil {
		t.Fatal(err)
	}

	if _, err := io.Copy(io.Discard, read); err != nil {
		t.Fatal(err)
	}
}

var digits = regexp.MustCompile(`\d+`)

// TestContractInteropChangedAsRead: what this tar says of a tree that
// changes while it is archived, and the status it exits with, are GNU
// tar's, the byte counts aside: they depend on how far the read had gone.
func TestContractInteropChangedAsRead(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the GNU tar on a Windows runner's PATH is Git for Windows', which fails on these paths")
	}

	system := reference.Find(t, "tar", "--version")

	version, err := exec.CommandContext(t.Context(), system, "--version").Output()
	if err != nil || !strings.Contains(string(version), "GNU tar") {
		t.Skip("the tar on PATH is not GNU tar")
	}

	for name, change := range changes() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			args := []string{"-cf", "-", "--sort=name", "-C"}

			root := changing(t)
			cmd := exec.CommandContext(t.Context(), system, append(args, root, "tree")...)

			var theirDiag bytes.Buffer

			cmd.Stderr = &theirDiag

			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}

			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}

			into(t, out, func() error { return change(filepath.Join(root, "tree")) })

			theirStatus := 0

			if err := cmd.Wait(); err != nil {
				exit, ok := errors.AsType[*exec.ExitError](err)
				if !ok {
					t.Fatal(err)
				}

				theirStatus = exit.ExitCode()
			}

			root = changing(t)
			read, write := io.Pipe()

			var ourDiag bytes.Buffer

			status := make(chan int, 1)

			go func() {
				status <- tarcmd.Run(append(args, root, "tree"), nil, write, &ourDiag, "test")

				_ = write.Close()
			}()

			into(t, read, func() error { return change(filepath.Join(root, "tree")) })

			theirs := digits.ReplaceAllString(strings.ReplaceAll(theirDiag.String(), system+": ", "tar: "), "N")
			ours := digits.ReplaceAllString(ourDiag.String(), "N")

			if ourStatus := <-status; ourStatus != theirStatus || ours != theirs {
				t.Errorf(
					"ours exits %d and says %q\nGNU tar exits %d and says %q",
					ourStatus,
					ours,
					theirStatus,
					theirs,
				)
			}
		})
	}
}

// TestContractChangedAsRead: a file that shrinks while it is read is
// filled with zeros to the size its header states, one that grows is cut
// at it, and both are said, as is a file whose time moved and a directory
// whose names did; the archive is whole, every member in it, and the run
// exits 1, which is neither success nor failure. The words are GNU tar's.
func TestContractChangedAsRead(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"left alone":           "",
		"the file shrank":      "tar: tree/big: File shrank by N bytes; padding with zeros\n",
		"the file grew":        "tar: tree/big: file changed as we read it\n",
		"the file was touched": "tar: tree/big: file changed as we read it\n",
		"a name came":          "tar: tree: file changed as we read it\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := changing(t)
			read, write := io.Pipe()

			var (
				diag    bytes.Buffer
				archive bytes.Buffer
			)

			status := make(chan int, 1)

			go func() {
				status <- tarcmd.Run([]string{"-cf", "-", "--sort=name", "-C", root, "tree"}, nil, write, &diag, "test")

				_ = write.Close()
			}()

			into(t, io.TeeReader(read, &archive), func() error { return changes()[name](filepath.Join(root, "tree")) })

			wantStatus := tarcmd.ExitChanged
			if want == "" {
				wantStatus = tarcmd.ExitOK
			}

			if got := <-status; got != wantStatus || digits.ReplaceAllString(diag.String(), "N") != want {
				t.Errorf("exit %d, %q; want %d, %q", got, diag.String(), wantStatus, want)
			}

			// The archive is whole: the names first listed, the large file
			// at the size its header states, and the end-of-archive blocks.
			reader := tar.NewReader(&archive)

			var entries []string

			for {
				header, err := reader.Next()
				if errors.Is(err, io.EOF) {
					break
				}

				if err != nil {
					t.Fatalf("the archive after %q: %v", entries, err)
				}

				size, err := io.Copy(io.Discard, reader)
				if err != nil || size != header.Size {
					t.Fatalf("%s: %d bytes of %d, %v", header.Name, size, header.Size, err)
				}

				entries = append(entries, header.Name)
			}

			if !slices.Equal(entries, []string{"tree/", "tree/a", "tree/big"}) {
				t.Errorf("entries %q", entries)
			}
		})
	}
}

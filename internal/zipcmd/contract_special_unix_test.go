//go:build unix

package zipcmd_test

// Unix alone: the test makes its FIFO with mkfifo, which Windows lacks.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/zipcmd"
)

// TestContractFIFOIsNotAnInput: a FIFO is not opened, which would block
// until something wrote to it: met under -r or reached through a link, it
// is skipped with a warning, the rest is stored and the status is 0.
func TestContractFIFOIsNotAnInput(t *testing.T) {
	tree(t)

	fifo := filepath.Join("d", "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(fifo, "link"); err != nil {
		t.Fatal(err)
	}

	type result struct {
		status int
		stderr string
	}

	done := make(chan result, 1)

	go func() {
		status, _, stderr := run(t, "", "-qr", "fifo.zip", "d", "link")
		done <- result{status, stderr}
	}()

	var got result

	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Error("zip is still blocked on the FIFO after 5 s")

		// Let it go: a writer that opens and closes ends a read. Without
		// blocking itself, which a writer does when nothing reads.
		for released := false; !released; {
			select {
			case got = <-done:
				released = true
			case <-time.After(10 * time.Millisecond):
				if writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
					_ = writer.Close()
				}
			}
		}
	}

	want := "\tzip warning: ignoring FIFO (Named Pipe): d/fifo\n" +
		"\tzip warning: ignoring FIFO (Named Pipe): link\n"
	if got.status != zipcmd.ExitOK || got.stderr != want {
		t.Errorf("exit %d, stderr %q, want 0 and %q", got.status, got.stderr, want)
	}

	if names := strings.Join(names(readArchiveFile(t, "fifo.zip")), " "); names != "d/ d/a.txt d/sub/ d/sub/b.txt" {
		t.Errorf("the archive holds %q, want \"d/ d/a.txt d/sub/ d/sub/b.txt\"", names)
	}
}

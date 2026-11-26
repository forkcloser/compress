//go:build unix

package tarcmd_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/forkcloser/go-compress/internal/tarcmd"
)

// TestContractArchiveToAFifoWaitsForItsReader: an archive written to a fifo
// reaches a reader that opens the fifo late. tar waits in the open, as any
// writer of a fifo does; it used to open the fifo for reading too, which
// waits for nobody, write a small archive into the fifo's buffer and end
// with status 0, and the reader found nothing.
func TestContractArchiveToAFifoWaitsForItsReader(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")

	if err := os.WriteFile(filepath.Join(dir, "member"), []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)

	go func() {
		status, _, _ := run(t, nil, "-cf", fifo, "-C", dir, "member")
		done <- status
	}()

	// A tar that does not wait is gone by now, its archive with it.
	select {
	case status := <-done:
		done <- status
	case <-time.After(300 * time.Millisecond):
	}

	// Not waiting for a writer: a tar that has ended must not hang the test.
	reader, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = reader.Close() }()

	// The open did not wait; the reads do, for as long as the fifo has a
	// writer, and end at once when it has none.
	if err = syscall.SetNonblock(int(reader.Fd()), false); err != nil {
		t.Fatal(err)
	}

	// With the fifo held open nothing written to it is lost, so a tar that
	// has not reached its open yet is read when it has: until its status
	// is in, and once more after.
	var archive []byte

	for ended := false; !ended; {
		select {
		case status := <-done:
			if status != tarcmd.ExitOK {
				t.Fatalf("tar -cf fifo: exit %d", status)
			}

			ended = true
		case <-time.After(10 * time.Millisecond):
		}

		part, readErr := io.ReadAll(reader)
		if readErr != nil {
			t.Fatal(readErr)
		}

		archive = append(archive, part...)
	}

	listing, _ := mustRun(t, archive, "-tf", "-")
	if strings.TrimSpace(listing) != "member" {
		t.Errorf("the reader of the fifo got %d bytes listing %q, want the archive of member", len(archive), listing)
	}
}

// TestContractArchiveThatMayOnlyBeWritten: an existing archive file its
// owner may write and not read is written, as creating an archive reads
// nothing of it. It was refused with "Cannot open: permission denied".
func TestContractArchiveThatMayOnlyBeWritten(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	archive := filepath.Join(dir, "out.tar")

	if err := os.WriteFile(filepath.Join(dir, "member"), []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Longer than the archive to come: what is left of it would show.
	if err := os.WriteFile(archive, make([]byte, 1<<16), 0o200); err != nil {
		t.Fatal(err)
	}

	mustRun(t, nil, "-cf", archive, "-C", dir, "member")

	if err := os.Chmod(archive, 0o600); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}

	fresh, _ := mustRun(t, nil, "-cf", "-", "-C", dir, "member")
	if string(written) != fresh {
		t.Errorf("the archive is %d bytes, want the %d of the same archive written anew", len(written), len(fresh))
	}
}

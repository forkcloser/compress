//go:build unix

package tarcmd_test

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
)

// TestContractArchiveThatIsNoRegularFile: an archive that is not a regular
// file is never a member of what it archives, and nothing is compared: a
// fifo archived into itself gets its entry, and the null device archived
// into itself is not said to contain itself.
func TestContractArchiveThatIsNoRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")

	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("no fifo here: %v", err)
	}

	// The read end is opened first and does not wait for a writer: the
	// archive is small enough to sit in the fifo until it is read.
	file, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = file.Close() }()

	_, stderr := mustRun(t, nil, "-cf", fifo, "-C", dir, "fifo")

	archive, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}

	if entries := names(t, archive); stderr != "" || !slices.Equal(entries, []string{"fifo"}) {
		t.Errorf("entries %q, stderr %q", entries, stderr)
	}

	if _, stderr := mustRun(t, nil, "-cf", os.DevNull, "--no-recursion", "-C", filepath.Dir(os.DevNull),
		filepath.Base(os.DevNull)); stderr != "" {
		t.Errorf("the null device into itself: %q", stderr)
	}
}

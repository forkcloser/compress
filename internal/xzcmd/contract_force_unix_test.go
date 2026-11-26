//go:build unix

package xzcmd_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestContractForceReplacesAFifo: a fifo at the counterpart's name is
// replaced under --force, not opened: nothing reads it, and an open to
// write would wait for that.
func TestContractForceReplacesAFifo(t *testing.T) {
	t.Parallel()

	plain := filepath.Join(t.TempDir(), "data")
	writeFile(t, plain, payload)

	if err := syscall.Mkfifo(plain+".xz", 0o644); err != nil {
		t.Skipf("no fifo here: %v", err)
	}

	mustRun(t, "xz", nil, "-kf", plain)

	if info, err := os.Lstat(plain + ".xz"); err != nil || !info.Mode().IsRegular() {
		t.Errorf("the counterpart: %v", err)
	}
}

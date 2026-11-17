//go:build unix

package xzcmd_test

import (
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/xzcmd"
)

// asCommand, set, makes this test binary the xz command: an interrupted
// run is a child process signalled inside the window.
const asCommand = "COMPRESS_TEST_RUN_AS_XZ"

func TestMain(m *testing.M) {
	if os.Getenv(asCommand) != "" {
		os.Exit(xzcmd.Run("xz", os.Args[1:], os.Stdin, os.Stdout, os.Stderr, "test"))
	}

	m.Run()
}

// TestContractInterruptedLeavesNoCounterpart: a run ended by SIGINT,
// SIGTERM or SIGHUP while it writes a counterpart removes what it wrote of
// it, leaves the input, and dies of that signal. A truncated file under
// the finished name is the one outcome a caller cannot tell from success
// by looking.
func TestContractInterruptedLeavesNoCounterpart(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "data")

	// Bytes that do not compress, enough of them that the run is still
	// writing when the signal lands.
	random := make([]byte, 16<<20)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(file, random, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if signal.Ignored(sig) {
			t.Logf("%v: this process was started ignoring it, and so would the child be", sig)

			continue
		}

		// The deadline kills a child the signal did not end.
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)

		cmd := exec.CommandContext(ctx, os.Args[0], "-9", file)

		cmd.Env = append(os.Environ(), asCommand+"=1")

		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}

		for ctx.Err() == nil {
			if info, err := os.Stat(file + ".xz"); err == nil && info.Size() > 0 {
				break
			}

			time.Sleep(time.Millisecond)
		}

		if err := cmd.Process.Signal(sig); err != nil {
			t.Errorf("%v: %v", sig, err)
		}

		err := cmd.Wait()

		cancel()

		exit, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("%v: the child ended with %v: the window was missed", sig, err)
		}

		if status, ok := exit.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != sig {
			t.Errorf("%v: the child ended with %v, not of the signal", sig, exit)
		}

		if _, err := os.Stat(file + ".xz"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%v: a partial data.xz is left (%v)", sig, err)

			_ = os.Remove(file + ".xz")
		}

		if info, err := os.Stat(file); err != nil || info.Size() != int64(len(random)) {
			t.Errorf("%v: the input: %v", sig, err)
		}
	}
}

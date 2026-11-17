//go:build unix

package filecodec

import (
	"os"
	"os/signal"
	"syscall"
)

// interrupts are the signals that end a run, less those the process was
// started ignoring: nohup's SIGHUP stays ignored, which watching it would
// undo.
func interrupts() []os.Signal {
	var caught []os.Signal

	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			caught = append(caught, sig)
		}
	}

	return caught
}

// die ends the process of the signal it caught, so that whoever waits for
// it sees the signal and not a status. It does not return.
func die(sig os.Signal) {
	signal.Reset(sig)

	if number, ok := sig.(syscall.Signal); ok {
		_ = syscall.Kill(syscall.Getpid(), number)
	}

	// The signal is delivered on a thread of its own choosing.
	select {}
}

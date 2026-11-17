//go:build !unix

package filecodec

import "os"

// interrupts are the signals that end a run: the one there is here.
func interrupts() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// die ends the process: there is no dying of a signal here, so with the
// status of an error. It does not return.
func die(os.Signal) {
	//revive:disable-next-line:deep-exit // the signal's own work: the run is over wherever it stands
	os.Exit(ExitError)
}

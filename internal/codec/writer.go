/*
   Copyright Mycophonic.
   Copyright Forkcloser.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package codec

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// errWriteClosed is a Write after Close.
var errWriteClosed = fmt.Errorf("%w: %w", ErrClosed, os.ErrClosed)

// Destination is the writer under an encoder, remembering its first failure.
type Destination struct {
	writer io.Writer
	err    error
}

func (d *Destination) Write(p []byte) (int, error) {
	n, err := d.writer.Write(p)
	if err != nil && d.err == nil {
		d.err = err
	}

	return n, err //nolint:wrapcheck // the destination's own error, which the encoder above reports and classify then names
}

// classify names an encoder's error: ErrDestination, with the destination's
// own failure in the chain when it had one. An encoder has no other way to
// fail but its destination, so the class is one.
func (d *Destination) classify(err error) error {
	if classed(err) {
		return err
	}

	if d.err != nil && !errors.Is(err, d.err) {
		return fmt.Errorf("%w: %w (the encoder reported: %w)", ErrDestination, d.err, err)
	}

	return fmt.Errorf("%w: %w", ErrDestination, err)
}

// Writer builds the format's encoder over w with raw and returns it with its
// errors classed and Write after Close enforced; Close flushes the stream's
// end and is idempotent.
func Writer(raw func(io.Writer) (io.WriteCloser, error), w io.Writer) (io.WriteCloser, error) {
	destination := &Destination{writer: w}

	encoder, err := raw(destination)
	if err != nil {
		return nil, destination.classify(err)
	}

	return &writer{encoder: encoder, destination: destination}, nil
}

type writer struct {
	encoder     io.WriteCloser
	destination *Destination
	closed      bool
}

func (w *writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errWriteClosed
	}

	n, err := w.encoder.Write(p)
	if err != nil {
		return n, w.destination.classify(err)
	}

	return n, nil
}

func (w *writer) Close() error {
	if w.closed {
		return nil
	}

	w.closed = true

	if err := w.encoder.Close(); err != nil {
		return w.destination.classify(err)
	}

	return nil
}

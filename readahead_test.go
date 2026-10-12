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

package compress_test

// ReadAhead's own contract: the decoder's bytes, order and errors, exactly;
// Close stops the goroutine whatever the state and closes the decoder once.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/forkcloser/compress"
)

// scripted is a decoder that hands out data in chunks of at most step, then
// ends with final (io.EOF or an error), and records its Close.
type scripted struct {
	data   []byte
	step   int
	final  error
	closed int
	closes error
}

func (s *scripted) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, s.final
	}

	n := copy(p, s.data[:min(len(s.data), s.step, len(p))])
	s.data = s.data[n:]

	return n, nil
}

func (s *scripted) Close() error {
	s.closed++

	return s.closes
}

// TestReadAheadBytesAndErrors: for every chunking of the decoder and every
// read size of the caller, the bytes are the decoder's, and the decoder's
// final error follows them.
func TestReadAheadBytesAndErrors(t *testing.T) {
	t.Parallel()

	want := payload(3*compress.MiB + 7)

	for _, step := range []int{1, 7, 4096, 64 * compress.KiB, compress.MiB, 2 * compress.MiB} {
		for _, final := range []error{io.EOF, io.ErrUnexpectedEOF, errBoom} {
			t.Run(strconv.Itoa(step)+"/"+final.Error(), func(t *testing.T) {
				t.Parallel()

				reader := compress.ReadAhead(&scripted{data: bytes.Clone(want), step: step, final: final})

				var got bytes.Buffer

				_, err := io.CopyBuffer(&got, reader, make([]byte, 1000)) // a caller's read size that matches no block
				if errors.Is(final, io.EOF) {
					if err != nil {
						t.Fatal(err)
					}
				} else if !errors.Is(err, final) {
					t.Fatalf("got %v, want %v", err, final)
				}

				if !bytes.Equal(got.Bytes(), want) {
					t.Fatalf("%d bytes, want %d", got.Len(), len(want))
				}

				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// TestReadAheadEmpty: a decoder with nothing is EOF at once, and its error
// alone is its error.
func TestReadAheadEmpty(t *testing.T) {
	t.Parallel()

	reader := compress.ReadAhead(&scripted{final: io.EOF})

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != 0 {
		t.Fatalf("%d bytes from an empty decoder", len(data))
	}

	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}

	reader = compress.ReadAhead(&scripted{final: errBoom})

	if _, err = io.ReadAll(reader); !errors.Is(err, errBoom) {
		t.Fatalf("got %v, want %v", err, errBoom)
	}

	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

// stalled is a decoder whose Read blocks until released, then ends.
type stalled struct {
	release chan struct{}
	closed  int
}

func (s *stalled) Read([]byte) (int, error) {
	<-s.release

	return 0, io.EOF
}

func (s *stalled) Close() error {
	s.closed++

	return nil
}

// eventually runs f and fails the test if it has not returned in time: a
// hang is the defect these tests exist for, and a hung test says nothing.
func eventually(t *testing.T, what string, f func()) {
	t.Helper()

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		f()
	}()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", what)
	}
}

// TestReadAheadClose: Close part-way returns, closes the decoder once with
// its error, is idempotent, and every Read after it is os.ErrClosed, blocks
// still queued or not.
func TestReadAheadClose(t *testing.T) {
	t.Parallel()

	decoder := &scripted{data: payload(64 * compress.MiB), step: compress.MiB, final: io.EOF, closes: errBoom}
	reader := compress.ReadAhead(decoder)

	buf := make([]byte, 10)

	if _, err := reader.Read(buf); err != nil { // started, blocks are in flight
		t.Fatal(err)
	}

	eventually(t, "Close", func() {
		if err := reader.Close(); !errors.Is(err, errBoom) {
			t.Errorf("first Close: got %v, want %v", err, errBoom)
		}

		if err := reader.Close(); err != nil {
			t.Errorf("second Close: %v", err)
		}
	})

	if decoder.closed != 1 {
		t.Fatalf("decoder closed %d times", decoder.closed)
	}

	eventually(t, "Read after Close", func() {
		for range 10 {
			if _, err := reader.Read(buf); !errors.Is(err, os.ErrClosed) {
				t.Errorf("got %v, want %v", err, os.ErrClosed)
			}
		}
	})
}

// TestReadAheadCloseUnread: a reader closed before any Read returns, closes
// the decoder, and reads as closed.
func TestReadAheadCloseUnread(t *testing.T) {
	t.Parallel()

	decoder := &scripted{data: payload(8 * compress.MiB), step: compress.MiB, final: io.EOF}
	reader := compress.ReadAhead(decoder)

	eventually(t, "Close", func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})

	if decoder.closed != 1 {
		t.Fatalf("decoder closed %d times", decoder.closed)
	}

	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("got %v, want %v", err, os.ErrClosed)
	}
}

// TestReadAheadCloseWhileParked: a Read parked on a decoder whose source has
// stalled returns os.ErrClosed when another goroutine closes the reader; Close
// itself returns once the decoder's Read does.
func TestReadAheadCloseWhileParked(t *testing.T) {
	t.Parallel()

	decoder := &stalled{release: make(chan struct{})}
	reader := compress.ReadAhead(decoder)

	readErr := make(chan error, 1)

	go func() {
		_, err := reader.Read(make([]byte, 1))
		readErr <- err
	}()

	time.Sleep(50 * time.Millisecond) // the Read is parked on an empty queue

	closeErr := make(chan error, 1)

	go func() { closeErr <- reader.Close() }()

	select {
	case err := <-readErr:
		if !errors.Is(err, os.ErrClosed) { // released by Close, before the decoder's Read returns
			t.Fatalf("got %v, want %v", err, os.ErrClosed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the parked Read did not return after Close")
	}

	close(decoder.release)

	select {
	case err := <-closeErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after the decoder's Read did")
	}

	if decoder.closed != 1 {
		t.Fatalf("decoder closed %d times", decoder.closed)
	}
}

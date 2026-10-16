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
	"sync"
	"sync/atomic"
)

// errReadClosed is a Read after Close.
var errReadClosed = fmt.Errorf("%w: %w", ErrClosed, os.ErrClosed)

// Source is the reader under a decoder, remembering its first failure other
// than its end: what tells a source that broke from a stream that is not
// what its format says, since the decoder reports both as its error.
type Source struct {
	reader io.Reader

	mu  sync.Mutex // a decoder may read on a goroutine of its own
	err error
}

// Track returns a Source over reader.
func Track(reader io.Reader) *Source {
	return &Source{reader: reader}
}

func (s *Source) Read(p []byte) (int, error) {
	n, err := s.reader.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		s.mu.Lock()

		if s.err == nil {
			s.err = err
		}

		s.mu.Unlock()
	}

	return n, err //nolint:wrapcheck // the source's own error, which the decoder above reports and Classify then names
}

// Failure is the source's first failure, or nil.
func (s *Source) Failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.err
}

// Classify names a decoder's error: ErrSource when the source failed under
// it, ErrCorrupt otherwise; one already classed is left alone.
func (s *Source) Classify(err error) error {
	return classify(err, s.Failure())
}

// classify is Classify against a source's recorded failure, if any.
func classify(err, src error) error {
	if classed(err) {
		return err
	}

	if src != nil {
		if errors.Is(err, src) {
			return fmt.Errorf("%w: %w", ErrSource, err)
		}

		return fmt.Errorf("%w: %w (the decoder reported: %w)", ErrSource, src, err)
	}

	return fmt.Errorf("%w: %w", ErrCorrupt, err)
}

// Reader builds the format's decoder over r with raw and returns it with
// its errors classed and Read after Close enforced.
func Reader(raw func(io.Reader) (io.ReadCloser, error), r io.Reader) (io.ReadCloser, error) {
	source := Track(r)

	decoder, err := raw(source)
	if err != nil {
		return nil, source.Classify(err)
	}

	return NewReader(decoder, source), nil
}

// SourceAt is Source for a reader read at offsets, which a parallel decoder
// does from several goroutines at once.
type SourceAt struct {
	reader io.ReaderAt

	mu  sync.Mutex
	err error
}

// TrackAt returns a SourceAt over reader.
func TrackAt(reader io.ReaderAt) *SourceAt {
	return &SourceAt{reader: reader}
}

// ReadAt reads from the source at off, remembering its first failure.
func (s *SourceAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := s.reader.ReadAt(p, off)
	if err != nil && !errors.Is(err, io.EOF) {
		s.mu.Lock()

		if s.err == nil {
			s.err = err
		}

		s.mu.Unlock()
	}

	return n, err //nolint:wrapcheck // the source's own error, which the decoder above reports and Classify then names
}

// Failure is the source's first failure, or nil.
func (s *SourceAt) Failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.err
}

// Classify is Source.Classify.
func (s *SourceAt) Classify(err error) error {
	return classify(err, s.Failure())
}

// ReaderAt builds the format's seekable decoder over r, size bytes long, with
// raw and returns it with its errors classed and Read after Close enforced.
func ReaderAt(
	raw func(io.ReaderAt, int64) (io.ReadCloser, error),
	r io.ReaderAt,
	size int64,
) (io.ReadCloser, error) {
	source := TrackAt(r)

	decoder, err := raw(source, size)
	if err != nil {
		return nil, source.Classify(err)
	}

	return &reader{decoder: decoder, source: source}, nil
}

// classifier names a decoder's error against its source's failure.
type classifier interface {
	Classify(err error) error
}

// NewReader wraps a decoder built over source: its Read and Close errors are
// classed against the source's failure, and a Read after Close is ErrClosed,
// which the decoders answer three different ways on their own. Close may
// come from another goroutine while a Read is parked on the decoder, so the
// flag is atomic.
func NewReader(decoder io.ReadCloser, source *Source) io.ReadCloser {
	return &reader{decoder: decoder, source: source}
}

type reader struct {
	decoder io.ReadCloser
	source  classifier
	closed  atomic.Bool
}

func (r *reader) Read(p []byte) (int, error) {
	if r.closed.Load() {
		return 0, errReadClosed
	}

	n, err := r.decoder.Read(p)
	if err == nil || errors.Is(err, io.EOF) {
		return n, err //nolint:wrapcheck // nil or io.EOF
	}

	return n, r.source.Classify(err)
}

func (r *reader) Close() error {
	if r.closed.Swap(true) {
		return nil
	}

	if err := r.decoder.Close(); err != nil {
		return r.source.Classify(err)
	}

	return nil
}

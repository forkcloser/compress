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
	"io"
	"sync"
)

const (
	// readAheadBlock is one decoded block; readAheadDepth is how many may
	// wait for the consumer. With the block in the decoder's hands and the
	// one in the caller's, six megabytes per stream.
	readAheadBlock = 1 << 20
	readAheadDepth = 4
)

// ReadAhead is compress.ReadAhead: the decoder read on a goroutine of its own,
// up to readAheadDepth blocks ahead of the caller. The bytes, their order and
// the decoder's errors are exactly the decoder's; a Read after Close, or
// parked when Close comes, is ErrClosed.
func ReadAhead(decoder io.ReadCloser) io.ReadCloser {
	ahead := &readAhead{
		decoder: decoder,
		blocks:  make(chan block, readAheadDepth),
		spare:   make(chan []byte, readAheadDepth+2),
		stop:    make(chan struct{}),
		closed:  make(chan struct{}),
		done:    make(chan struct{}),
	}

	// Every buffer the stream can hold at once: one with the decoder, depth
	// in blocks, one with the caller; the spares channel never blocks a return.
	for range readAheadDepth + 2 {
		ahead.spare <- make([]byte, readAheadBlock)
	}

	go ahead.decode()

	return ahead
}

type block struct {
	data []byte // a spare, filled; nil with err
	err  error
}

type readAhead struct {
	decoder io.ReadCloser
	blocks  chan block
	spare   chan []byte
	stop    chan struct{} // closed by Close, first: the goroutine returns
	closed  chan struct{} // closed by Close: a Read returns ErrClosed
	done    chan struct{} // closed by the goroutine on its way out

	current []byte // the block being handed out, a spare
	rest    []byte // what is left of it
	err     error  // the decoder's end, once seen
	once    sync.Once
}

func (r *readAhead) Read(p []byte) (int, error) {
	select {
	case <-r.closed:
		return 0, errReadClosed
	default:
	}

	if len(r.rest) == 0 {
		if r.err != nil {
			return 0, r.err
		}

		if r.current != nil {
			r.spare <- r.current[:cap(r.current)]

			r.current = nil
		}

		// Not done: it closes on a normal end too, with blocks still queued.
		var next block
		select {
		case next = <-r.blocks:
		case <-r.closed:
			return 0, errReadClosed
		}

		if next.err != nil {
			r.err = next.err

			return 0, r.err
		}

		r.current, r.rest = next.data, next.data
	}

	n := copy(p, r.rest)
	r.rest = r.rest[n:]

	return n, nil
}

// Close stops the decoding goroutine, waits for it, and closes the decoder.
func (r *readAhead) Close() error {
	var err error

	r.once.Do(func() {
		// The goroutine is told before a Read is: a Read that answers
		// ErrClosed is one the goroutine reads no further under.
		close(r.stop)
		close(r.closed)
		<-r.done

		err = r.decoder.Close()
	})

	return err //nolint:wrapcheck // the decoder's close error, as the decoder reports it
}

// decode fills spares from the decoder and sends them in order, until the
// decoder ends or Close says stop.
func (r *readAhead) decode() {
	defer close(r.done)

	for {
		// Stop comes first: a select that also holds a spare would take it
		// half the time, and Close would wait for one more Read.
		select {
		case <-r.stop:
			return
		default:
		}

		var buf []byte

		select {
		case buf = <-r.spare:
		case <-r.stop:
			return
		}

		n, err := r.fill(buf)

		if n > 0 {
			select {
			case r.blocks <- block{data: buf[:n]}:
			case <-r.stop:
				return
			}
		}

		if err != nil {
			select {
			case r.blocks <- block{err: err}:
			case <-r.stop:
			}

			return
		}
	}
}

// fill reads into buf until it is full or the decoder ends, and no longer
// than the caller can wait: it returns what it has once Close has come, and
// once the caller has nothing queued, so that Close waits for the decoder's
// current Read and no more, and bytes the decoder has given are not held
// for a block to fill. A caller that is behind still gets whole blocks. Not
// io.ReadFull: that reports a decoder's own io.ErrUnexpectedEOF (a
// truncated stream) and its own short read at a clean end the same way, and
// the two must differ.
func (r *readAhead) fill(buf []byte) (int, error) {
	n := 0

	for n < len(buf) {
		m, err := r.decoder.Read(buf[n:])
		n += m

		if err != nil {
			return n, err //nolint:wrapcheck // the decoder's error, as the decoder reports it
		}

		select {
		case <-r.stop:
			return n, nil
		default:
		}

		if n > 0 && len(r.blocks) == 0 {
			return n, nil
		}
	}

	return n, nil
}

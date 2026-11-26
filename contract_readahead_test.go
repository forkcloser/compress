package compress_test

import (
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/forkcloser/go-compress"
)

// stepped gives one byte a Read, each when it is allowed one, and says
// when a Read is entered.
type stepped struct {
	entered chan struct{}
	allow   chan struct{}
}

func (d *stepped) Read(p []byte) (int, error) {
	d.entered <- struct{}{}

	<-d.allow

	p[0] = 'x'

	return 1, nil
}

func (*stepped) Close() error { return nil }

// message gives its text once, then waits to be released, as a decoder over
// a source that has gone quiet does.
type message struct {
	text    string
	release chan struct{}
}

func (d *message) Read(p []byte) (int, error) {
	if d.text == "" {
		<-d.release

		return 0, io.EOF
	}

	n := copy(p, d.text)
	d.text = d.text[n:]

	return n, nil
}

func (*message) Close() error { return nil }

// TestContractReadAheadCloseWaitsForOneRead: "Close stops the goroutine,
// once the decoder's current Read returns": that Read, and not the ones a
// block would take to fill. With the decoder parked in a Read when Close
// comes, Close returns when that Read does, and the decoder is not read
// again.
func TestContractReadAheadCloseWaitsForOneRead(t *testing.T) {
	t.Parallel()

	// Room for the Reads this test never lets in, so that one entered
	// against the contract is counted and not parked.
	decoder := &stepped{entered: make(chan struct{}, 8), allow: make(chan struct{})}
	reader := compress.ReadAhead(decoder)

	decoder.allow <- struct{}{}

	// The first Read gave its byte; the decoder is in its second.
	eventually(t, "the Read of the decoder's first byte", func() {
		_, _ = io.ReadFull(reader, make([]byte, 1))

		<-decoder.entered
		<-decoder.entered
	})

	closed := make(chan error, 1)

	go func() { closed <- reader.Close() }()

	// A Read that answers ErrClosed: Close has come, and waits.
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, compress.ErrClosed) {
		t.Fatalf("a Read under Close: %v", err)
	}

	decoder.allow <- struct{}{}

	eventually(t, "Close, once the decoder's current Read returned", func() {
		if err := <-closed; err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	if more := len(decoder.entered); more != 0 {
		t.Errorf("the decoder was read %d more times after the Read Close waited for", more)
	}
}

// TestContractReadAheadDeliversWhatTheDecoderGave: the bytes are "exactly
// the decoder's", and they are the caller's once the decoder has given
// them: a message the decoder delivered is read while the decoder waits for
// more, not held until a block fills.
func TestContractReadAheadDeliversWhatTheDecoderGave(t *testing.T) {
	t.Parallel()

	const text = "first message\n"

	decoder := &message{text: text, release: make(chan struct{})}
	reader := compress.ReadAhead(decoder)

	buf := make([]byte, len(text))

	eventually(t, "the Read of a message the decoder gave, while the decoder waits", func() {
		_, _ = io.ReadFull(reader, buf)
	})

	if string(buf) != text {
		t.Errorf("delivered %q", buf)
	}

	close(decoder.release)

	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

// paced gives one byte a Read and counts the Reads entered; with turns, a
// Read waits for its turn.
type paced struct {
	entered atomic.Int64
	turns   chan struct{}
}

func (d *paced) Read(p []byte) (int, error) {
	d.entered.Add(1)

	if d.turns != nil {
		<-d.turns
	}

	p[0] = 'x'

	return 1, nil
}

func (*paced) Close() error { return nil }

// TestContractReadAheadCloseWaitsForOneReadWithABlockQueued: "Close stops
// the goroutine, once the decoder's current Read returns", for a caller
// that is behind as well: a block waits in the queue, the decoder is parked
// in its second Read when Close comes, and Close returns when that Read
// does, the decoder not read again. Many times over: a goroutine that only
// often looked at Close first would pass a single run.
func TestContractReadAheadCloseWaitsForOneReadWithABlockQueued(t *testing.T) {
	t.Parallel()

	for range 64 {
		synctest.Test(t, func(t *testing.T) {
			decoder := &paced{turns: make(chan struct{})}
			reader := compress.ReadAhead(decoder)

			// Whatever a broken goroutine still reads is let through at the end.
			defer close(decoder.turns)

			decoder.turns <- struct{}{}

			synctest.Wait()

			if entered := decoder.entered.Load(); entered != 2 {
				t.Fatalf("the decoder is in Read %d with its first byte queued, want 2", entered)
			}

			closed := make(chan error, 1)

			go func() { closed <- reader.Close() }()

			synctest.Wait()

			decoder.turns <- struct{}{}

			synctest.Wait()

			select {
			case err := <-closed:
				if err != nil {
					t.Errorf("Close: %v", err)
				}
			default:
				t.Error("Close has not returned, and the Read it waited for has")
			}

			if entered := decoder.entered.Load(); entered != 2 {
				t.Errorf("the decoder was read %d more times after the Read Close waited for", entered-2)
			}
		})
	}
}

// TestContractReadAheadGivesACallerBehindWholeBlocks: "a caller that is
// behind still gets whole blocks": from a decoder that gives one byte a
// Read, the first byte is handed over at once, and a caller that comes
// after the decoder has run ahead reads a megabyte at a time, not a byte.
func TestContractReadAheadGivesACallerBehindWholeBlocks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		reader := compress.ReadAhead(&paced{})

		// The decoder runs as far ahead as it may.
		synctest.Wait()

		buf := make([]byte, 2<<20)

		for i, want := range []int{1, 1 << 20, 1 << 20} {
			n, err := reader.Read(buf)
			if n != want || err != nil {
				t.Errorf("Read %d: %d bytes, %v; want %d", i+1, n, err, want)
			}
		}

		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
}

package compress_test

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/constants"
)

// stalledAt is a seekable input whose reads past from park until released,
// but for its last kilobyte, where the index is.
type stalledAt struct {
	*bytes.Reader

	from    int64
	release chan struct{}
}

func (s *stalledAt) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > s.from && off < s.Size()-constants.KiB {
		<-s.release
	}

	return s.Reader.ReadAt(p, off)
}

// TestContractReadThatCloseCutShortIsClosed: a Read parked on its input
// when another goroutine closes the reader comes back ErrClosed, on the
// seekable path as on the stream's, and never ErrCorrupt: the stream is
// whole, and the caller is the one who ended it.
func TestContractReadThatCloseCutShortIsClosed(t *testing.T) {
	t.Parallel()

	whole := xzBlocks(t, payload(4*constants.MiB+1), 256*constants.KiB)
	input := &stalledAt{Reader: bytes.NewReader(whole), from: int64(len(whole) / 2), release: make(chan struct{})}

	reader, err := compress.Decompress(input)
	if err != nil {
		t.Fatal(err)
	}

	readErr := make(chan error, 1)

	go func() {
		_, err := io.Copy(io.Discard, reader) // drains what was decoded, then parks on the input
		readErr <- err
	}()

	time.Sleep(100 * time.Millisecond) // the Read is parked

	closeErr := make(chan error, 1)

	go func() { closeErr <- reader.Close() }()

	select {
	case err := <-readErr:
		if !errors.Is(err, compress.ErrClosed) || errors.Is(err, compress.ErrCorrupt) {
			t.Errorf("the Read that Close cut short returned %v; want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("the parked Read did not return after Close")
	}

	close(input.release)

	select {
	case err := <-closeErr:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Close did not return after the input did")
	}
}

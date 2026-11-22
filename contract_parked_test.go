package compress_test

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/constants"
)

// TestContractCloseUnderAParkedRead: for zstd and xz as for gzip, a Read
// parked on a source that hangs comes back ErrClosed when another goroutine
// closes the reader, and Close returns once the source does; neither is
// left parked, and the two do not race. (The race lane is what holds the
// last: zstd's decoder is not safe under a Close of its own.)
func TestContractCloseUnderAParkedRead(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"zstd", "xz", "gzip"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			whole := encoders()[name](t, payload(4*constants.MiB+1))
			source := &stallingSource{data: whole[:len(whole)-2], release: make(chan struct{})}

			reader, err := compress.Decompress(source)
			if err != nil {
				t.Fatal(err)
			}

			readErr := make(chan error, 1)

			go func() {
				_, err := io.Copy(io.Discard, reader) // drains what was decoded, then parks on the stall
				readErr <- err
			}()

			time.Sleep(50 * time.Millisecond) // the Read is parked

			closeErr := make(chan error, 1)

			go func() { closeErr <- reader.Close() }()

			select {
			case err := <-readErr:
				if !errors.Is(err, compress.ErrClosed) {
					t.Fatalf("the parked Read returned %v, want ErrClosed", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the parked Read did not return after Close")
			}

			close(source.release)

			select {
			case err := <-closeErr:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Close did not return after the source did")
			}
		})
	}
}

package compress_test

import (
	"bytes"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/constants"
	"github.com/forkcloser/compress/lz4"
)

// TestContractLz4ClosedBeforeItsEndLeavesNothing: "the caller must close
// the returned ReadCloser to release decoder resources", and Close does
// release them: twenty lz4 readers read for a byte and closed leave no
// goroutine behind and nothing on the heap, through lz4.NewReader and
// through Decompress. It runs alone: it counts the process's goroutines
// and measures its heap.
//
//nolint:paralleltest // counts the process's goroutines and heap; a parallel test's would be counted
func TestContractLz4ClosedBeforeItsEndLeavesNothing(t *testing.T) {
	var stream bytes.Buffer

	writer, err := lz4.NewWriter(&stream)
	if err != nil {
		t.Fatal(err)
	}

	write(t, writer, payload(32*constants.MiB))

	live := func() uint64 {
		//revive:disable-next-line:call-to-gc what the readers left is measured from a collected heap
		runtime.GC()

		var stats runtime.MemStats

		runtime.ReadMemStats(&stats)

		return stats.HeapAlloc
	}

	for name, open := range map[string]func(io.Reader) (io.ReadCloser, error){
		"lz4.NewReader": lz4.NewReader,
		// Not a seekable input: the stream path.
		"Decompress": func(r io.Reader) (io.ReadCloser, error) { return compress.Decompress(io.MultiReader(r)) },
	} {
		goroutines, heap := runtime.NumGoroutine(), live()

		for range 20 {
			reader, err := open(bytes.NewReader(stream.Bytes()))
			if err != nil {
				t.Fatal(err)
			}

			if _, err := io.ReadFull(reader, make([]byte, 1)); err != nil {
				t.Fatal(err)
			}

			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
		}

		// A goroutine on its way out is given the time to go.
		left := runtime.NumGoroutine() - goroutines
		for wait := 0; left > 0 && wait < 100; wait++ {
			time.Sleep(10 * time.Millisecond)

			left = runtime.NumGoroutine() - goroutines
		}

		if grown := int64(live()) - int64(heap); left > 0 || grown > 8*constants.MiB {
			t.Errorf("%s: %d goroutines and %d MiB outlive twenty readers closed", name, left, grown>>20)
		}
	}
}

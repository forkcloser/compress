package compress_test

import (
	"errors"
	"io"
	"testing"

	"github.com/forkcloser/go-compress"
)

var errBroken = errors.New("connection reset")

// thenFail gives its bytes and then fails, as a connection that resets does.
type thenFail struct {
	data []byte
	err  error
}

func (r *thenFail) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}

	n := copy(p, r.data)
	r.data = r.data[n:]

	return n, nil
}

// TestContractSourceFailsInsideTheMagic: "a source that fails, here or
// later, is ErrSource", its error in the chain, however little it gave
// first: nothing, a part of a format's magic, or bytes that are no
// format's. It is never ErrUnknownFormat, which says the input is not for
// this package: a stream cut by its source has said nothing of its format.
func TestContractSourceFailsInsideTheMagic(t *testing.T) {
	t.Parallel()

	for _, given := range [][]byte{
		nil,
		{0x28},
		{0x28, 0xB5, 0x2F},             // zstd's, short
		{0x1F},                         // gzip's, short
		{0xFD, 0x37, 0x7A, 0x58, 0x5A}, // xz's, short
		{'a', 'b'},
	} {
		reader, err := compress.Decompress(&thenFail{data: given, err: errBroken})
		if reader != nil || !errors.Is(err, compress.ErrSource) || !errors.Is(err, errBroken) ||
			errors.Is(err, compress.ErrUnknownFormat) {
			t.Errorf("after %x: %v", given, err)
		}
	}

	// The same bytes from a source that ends are an unknown format's, or a
	// stream cut short, never the source's failure.
	for _, given := range [][]byte{{0x28, 0xB5, 0x2F}, {'a', 'b'}} {
		_, err := compress.Decompress(&thenFail{data: given, err: io.EOF})
		if !errors.Is(err, compress.ErrUnknownFormat) || errors.Is(err, compress.ErrSource) {
			t.Errorf("%x and the end: %v", given, err)
		}
	}
}

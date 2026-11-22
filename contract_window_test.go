package compress_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/zstd"
)

// windowFrame is a zstd frame of one raw block, "data\n", that declares
// the window the descriptor byte names: 0x88 is 128 MiB, 0x89 an eighth
// more.
func windowFrame(descriptor byte) []byte {
	return append([]byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, descriptor, 0x29, 0x00, 0x00}, "data\n"...)
}

// TestContractZstdWindow: the limit the commands hold a frame's window to
// is theirs alone. zstd.NewReader and Decompress decode a frame that
// declares 128 MiB and one that declares more, as they did.
func TestContractZstdWindow(t *testing.T) {
	t.Parallel()

	for name, open := range map[string]func(io.Reader) (io.ReadCloser, error){
		"zstd.NewReader": zstd.NewReader,
		"Decompress":     compress.Decompress,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, descriptor := range []byte{0x88, 0x89} {
				// Not a seekable input: the stream path is the one under test.
				reader, err := open(io.MultiReader(bytes.NewReader(windowFrame(descriptor))))
				if err != nil {
					t.Fatalf("descriptor %#x: %v", descriptor, err)
				}

				got, err := io.ReadAll(reader)
				if err != nil || string(got) != "data\n" {
					t.Errorf("descriptor %#x: %q, %v", descriptor, got, err)
				}

				if err := reader.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}
		})
	}
}

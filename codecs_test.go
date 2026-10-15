package compress_test

// The codec packages' contract: each format's NewWriter produces a stream its
// own library's reader decodes, NewReader decodes what the library's writer
// produced, and both answer with compress's classes: ErrDestination for a
// writer whose destination fails, ErrClosed for a Write or Read after Close,
// ErrCorrupt and ErrSource as Decompress does.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	kgzip "github.com/klauspost/compress/gzip"
	kzstd "github.com/klauspost/compress/zstd"
	plz4 "github.com/pierrec/lz4/v4"

	uxz "github.com/forkcloser/xz"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/bzip2"
	"github.com/forkcloser/compress/gzip"
	"github.com/forkcloser/compress/internal/constants"
	"github.com/forkcloser/compress/lz4"
	"github.com/forkcloser/compress/xz"
	"github.com/forkcloser/compress/zstd"
)

// codecPackage is one format package with the library it wraps, the oracle
// on the other side of each exchange.
type codecPackage struct {
	newReader func(io.Reader) (io.ReadCloser, error)
	newWriter func(io.Writer) (io.WriteCloser, error)
	libDecode func(io.Reader) (io.Reader, error)
	libEncode func(io.Writer) (io.WriteCloser, error)
}

func codecPackages() map[string]codecPackage {
	return map[string]codecPackage{
		"gzip": {
			newReader: gzip.NewReader, newWriter: gzip.NewWriter,
			libDecode: func(r io.Reader) (io.Reader, error) { return kgzip.NewReader(r) },
			libEncode: func(w io.Writer) (io.WriteCloser, error) { return kgzip.NewWriter(w), nil },
		},
		"xz": {
			newReader: xz.NewReader, newWriter: xz.NewWriter,
			libDecode: func(r io.Reader) (io.Reader, error) { return uxz.NewReader(r) },
			libEncode: func(w io.Writer) (io.WriteCloser, error) { return uxz.NewWriter(w) },
		},
		"lz4": {
			newReader: lz4.NewReader, newWriter: lz4.NewWriter,
			libDecode: func(r io.Reader) (io.Reader, error) { return plz4.NewReader(r), nil },
			libEncode: func(w io.Writer) (io.WriteCloser, error) { return plz4.NewWriter(w), nil },
		},
		"zstd": {
			newReader: zstd.NewReader, newWriter: zstd.NewWriter,
			libDecode: func(r io.Reader) (io.Reader, error) { return kzstd.NewReader(r) },
			libEncode: func(w io.Writer) (io.WriteCloser, error) { return kzstd.NewWriter(w) },
		},
	}
}

// TestCodecExchange: for every format and a few sizes, our writer's stream
// through the library's reader, and the library's writer's stream through
// our reader, both back whole.
func TestCodecExchange(t *testing.T) {
	t.Parallel()

	for name, pkg := range codecPackages() {
		for _, size := range []int{0, 1, 4*constants.KiB + 1, 256*constants.KiB + 1} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				want := payload(size)

				var ours bytes.Buffer

				writer, err := pkg.newWriter(&ours)
				if err != nil {
					t.Fatal(err)
				}

				write(t, writer, want)

				libReader, err := pkg.libDecode(bytes.NewReader(ours.Bytes()))
				if err != nil {
					t.Fatalf("size %d: the library refused our stream: %v", size, err)
				}

				if got, readErr := io.ReadAll(libReader); readErr != nil || !bytes.Equal(got, want) {
					t.Fatalf("size %d: the library read %d bytes of our stream, %v", size, len(got), readErr)
				}

				var theirs bytes.Buffer

				libWriter, err := pkg.libEncode(&theirs)
				if err != nil {
					t.Fatal(err)
				}

				write(t, libWriter, want)

				reader, err := pkg.newReader(bytes.NewReader(theirs.Bytes()))
				if err != nil {
					t.Fatalf("size %d: NewReader refused the library's stream: %v", size, err)
				}

				got, err := io.ReadAll(reader)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("size %d: NewReader read %d bytes of the library's stream, %v", size, len(got), err)
				}

				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// failingWriter accepts nothing.
type failingWriter struct{ err error }

func (w *failingWriter) Write([]byte) (int, error) { return 0, w.err }

// TestCodecWriterErrors: a destination that fails is ErrDestination with its
// error in the chain, on the Write or the Close that reaches it; a Write
// after Close is ErrClosed; a second Close is nil.
func TestCodecWriterErrors(t *testing.T) {
	t.Parallel()

	for name, pkg := range codecPackages() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// An encoder that writes its header at construction (xz) meets the
			// failure in NewWriter; the others on Write or Close.
			writer, newErr := pkg.newWriter(&failingWriter{err: errBoom})

			var writeErr, closeErr error
			if newErr == nil {
				_, writeErr = writer.Write(payload(constants.MiB))
				closeErr = writer.Close()
			}

			if newErr == nil && writeErr == nil && closeErr == nil {
				t.Fatal("a failing destination went unreported")
			}

			for _, err := range []error{newErr, writeErr, closeErr} {
				if err != nil && (!errors.Is(err, compress.ErrDestination) || !errors.Is(err, errBoom)) {
					t.Fatalf("got %v, want ErrDestination wrapping %v", err, errBoom)
				}
			}

			var buf bytes.Buffer

			writer, err := pkg.newWriter(&buf)
			if err != nil {
				t.Fatal(err)
			}

			write(t, writer, payload(10))

			if _, err := writer.Write([]byte("x")); !errors.Is(err, compress.ErrClosed) {
				t.Fatalf("Write after Close: %v, want ErrClosed", err)
			}

			if err := writer.Close(); err != nil {
				t.Fatalf("second Close: %v", err)
			}
		})
	}
}

// TestCodecReaderErrors: NewReader classes as Decompress does, a corrupt body
// ErrCorrupt and a broken source ErrSource, and a Read after Close is
// ErrClosed.
func TestCodecReaderErrors(t *testing.T) {
	t.Parallel()

	for name, pkg := range codecPackages() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			libWriter, err := pkg.libEncode(&buf)
			if err != nil {
				t.Fatal(err)
			}

			write(t, libWriter, payload(2*constants.MiB+1))

			whole := buf.Bytes()

			corrupt := bytes.Clone(whole)
			for i := 8; i < len(corrupt); i += 97 {
				corrupt[i] ^= 0x5A
			}

			if reader, newErr := pkg.newReader(bytes.NewReader(corrupt)); newErr != nil {
				if !errors.Is(newErr, compress.ErrCorrupt) {
					t.Fatalf("corrupt: NewReader %v, want ErrCorrupt", newErr)
				}
			} else if _, readErr := io.ReadAll(reader); !errors.Is(readErr, compress.ErrCorrupt) || errors.Is(readErr, compress.ErrSource) {
				t.Fatalf("corrupt: %v, want ErrCorrupt and not ErrSource", readErr)
			}

			reader, err := pkg.newReader(&halfThenFail{data: whole, err: errBoom})
			if err != nil {
				t.Fatal(err)
			}

			if _, err := io.ReadAll(reader); !errors.Is(err, compress.ErrSource) || !errors.Is(err, errBoom) {
				t.Fatalf("broken source: %v, want ErrSource wrapping %v", err, errBoom)
			}

			if err := reader.Close(); err != nil && !errors.Is(err, compress.ErrSource) {
				t.Fatalf("Close after the source broke: %v", err)
			}

			if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, compress.ErrClosed) {
				t.Fatalf("Read after Close: %v, want ErrClosed", err)
			}
		})
	}
}

// TestBzip2Reader: Go has no bzip2 writer, so the reader is checked from the
// fixture, and the package offers no NewWriter by design.
func TestBzip2Reader(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "payload.bz2"))
	if err != nil {
		t.Fatal(err)
	}

	reader, err := bzip2.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if got, err := io.ReadAll(reader); err != nil || string(got) != testPayload {
		t.Fatalf("got %q, %v", got, err)
	}

	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

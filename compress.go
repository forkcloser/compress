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

package compress

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"

	"github.com/forkcloser/compress/internal/codec"
	"github.com/forkcloser/compress/internal/constants"
)

// inputBufSize amortizes syscalls for every format.
const inputBufSize = 256 * constants.KiB

// Decompressor creates a decompressed reader from a compressed stream.
type Decompressor func(io.Reader) (io.ReadCloser, error)

// SeekableDecompressor creates a decompressed reader from a compressed file
// of a known size that can be read at any offset: a format whose layout
// rewards that (an index at the end, independent blocks) decodes it in
// parallel.
type SeekableDecompressor func(io.ReaderAt, int64) (io.ReadCloser, error)

// format is a registered decompression format.
type format struct {
	name  string
	magic []byte
	new   Decompressor
	newAt SeekableDecompressor // nil when seekability buys the format nothing
}

//nolint:gochecknoglobals // the format registry is process-wide by design
var (
	formatsMu sync.RWMutex
	formats   []format
	peekSize  int
)

// Register makes a decompression format available to Decompress.
// It is typically called from an init function in format-specific sub-packages.
func Register(name string, magic []byte, fn Decompressor) {
	RegisterSeekable(name, magic, fn, nil)
}

// RegisterSeekable is Register for a format that also decodes a seekable
// input of known size through at, which Decompress takes on its own when
// the input offers it; a nil at is Register.
func RegisterSeekable(name string, magic []byte, fn Decompressor, at SeekableDecompressor) {
	formatsMu.Lock()
	defer formatsMu.Unlock()

	formats = append(formats, format{name: name, magic: magic, new: fn, newAt: at})

	if len(magic) > peekSize {
		peekSize = len(magic)
	}
}

//nolint:gochecknoinits // Registration pattern requires init.
func init() {
	Register("zstd", []byte{0x28, 0xB5, 0x2F, 0xFD}, codec.Zstd)
}

// Compress returns a reader that yields zstd-compressed data from the input.
// A failure of the input is ErrSource on the read; the caller must close the
// returned ReadCloser to release the encoder, and a Read after that is
// ErrClosed.
func Compress(reader io.Reader) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()
	source := codec.Track(reader)

	// NewWriter fails only on an option, and none is passed.
	encoder, err := zstd.NewWriter(pipeWriter)
	if err != nil {
		panic("compress: zstd.NewWriter without options: " + err.Error())
	}

	go func() {
		_, copyErr := io.Copy(encoder, source)
		closeErr := encoder.Close()

		_ = pipeWriter.CloseWithError(compressFailure(source, copyErr, closeErr))
	}()

	return &compressed{reader: pipeReader}
}

// compressFailure names what ended the encoding: the source's failure, or the
// consumer closing the pipe under the encoder, whose writes are then
// io.ErrClosedPipe. Nil for a clean end.
func compressFailure(source *codec.Source, copyErr, closeErr error) error {
	if copyErr != nil {
		if source.Failure() != nil {
			return fmt.Errorf("%w: %w", ErrSource, copyErr)
		}

		return fmt.Errorf("%w: %w", ErrClosed, copyErr)
	}

	if closeErr != nil {
		return fmt.Errorf("%w: %w", ErrClosed, closeErr)
	}

	return nil
}

// compressed is Compress's result: the pipe, whose own closed error is named.
type compressed struct {
	reader *io.PipeReader
}

func (c *compressed) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if errors.Is(err, io.ErrClosedPipe) {
		return n, fmt.Errorf("%w: %w", ErrClosed, err)
	}

	return n, err //nolint:wrapcheck // nil, io.EOF, or what compressFailure named
}

func (c *compressed) Close() error {
	return c.reader.Close() //nolint:wrapcheck // a pipe reader's Close is always nil
}

// Decompress auto-detects the compression format from magic bytes and returns
// a streaming decompressed reader. Only formats that have been registered
// (via Register or sub-package init) are recognized: anything else is
// ErrUnknownFormat; an empty stream is ErrCorrupt, as is one that its
// decoder refuses or that ends early, here or on a later Read; a source that
// fails, here or later, is ErrSource. The caller must close the returned
// ReadCloser to release decoder resources; a Read after that is ErrClosed.
//
// An input that is both an io.ReaderAt and an io.Seeker (an *os.File, a
// *bytes.Reader, an *io.SectionReader) is decoded from its current offset to
// its end, and a format registered with a seekable decoder (xz, whose block
// index sits at the end) decodes it in parallel, the way io.Copy takes a
// faster path when the operands offer one. The input's offset is left where
// it was; its bytes are read at offsets, never through Read. A size the
// input misreports shows up as the stream cut short or as a failure of the
// input, ErrCorrupt or ErrSource.
func Decompress(reader io.Reader) (io.ReadCloser, error) {
	formatsMu.RLock()

	fmts := formats
	peek := peekSize

	formatsMu.RUnlock()

	if peek == 0 {
		return nil, fmt.Errorf("%w: no decompression format registered", ErrUnknownFormat)
	}

	ext, ok, err := seekable(reader)
	if err != nil {
		return nil, err
	}

	if ok {
		decoder, atErr := decompressSeekable(fmts, peek, ext)
		if !errors.Is(atErr, errStreamPath) {
			return decoder, atErr
		}
	}

	// Buffer the input to batch syscalls for decompressors that make many small reads.
	source := codec.Track(reader)
	buffered := bufio.NewReaderSize(source, inputBufSize)

	header, err := buffered.Peek(peek)
	if err != nil && len(header) == 0 {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: empty stream: %w", ErrCorrupt, io.ErrUnexpectedEOF)
		}

		return nil, fmt.Errorf("%w: %w", ErrSource, err)
	}

	for _, f := range fmts {
		if bytes.HasPrefix(header, f.magic) {
			decoder, err := f.new(buffered)
			if err != nil {
				return nil, source.Classify(err)
			}

			return codec.NewReader(decoder, source), nil
		}
	}

	return nil, fmt.Errorf("%w (header: %x)", ErrUnknownFormat, header)
}

// errStreamPath says a seekable input is the stream path's after all: its
// format has no seekable decoder, or no format opens it, and the stream
// path serves or reports it as it does any input.
var errStreamPath = errors.New("the stream path's")

// decompressSeekable is Decompress over a seekable input: the magic read at
// the offset, the format's seekable decoder over the extent, or errStreamPath.
func decompressSeekable(fmts []format, peek int, ext extent) (io.ReadCloser, error) {
	header, err := headerAt(ext, peek)
	if err != nil {
		return nil, err
	}

	f, ok := seekableFormat(fmts, header)
	if !ok {
		return nil, errStreamPath
	}

	return codec.ReaderAt(f.newAt, io.NewSectionReader(ext.readerAt, ext.offset, ext.size), ext.size)
}

// extent is a seekable input: readable at offsets, from offset for size
// bytes.
type extent struct {
	readerAt io.ReaderAt
	offset   int64
	size     int64
}

// seekable reports whether reader can be read at offsets with a known
// extent, and that extent: from its current offset to its end. A Seek that
// fails before the offset moved leaves the reader where it was, and the
// stream path takes it; one that fails to restore the offset after the end
// was measured leaves the reader at its end, with nothing for the stream
// path to read, and is ErrSource.
func seekable(reader io.Reader) (extent, bool, error) {
	readerAt, isAt := reader.(io.ReaderAt)

	seeker, isSeeker := reader.(io.Seeker)
	if !isAt || !isSeeker {
		return extent{}, false, nil
	}

	offset, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return extent{}, false, nil //nolint:nilerr // the offset did not move: not seekable after all, the stream path's
	}

	end, err := seeker.Seek(0, io.SeekEnd)
	if err != nil {
		return extent{}, false, nil //nolint:nilerr // likewise: a Seek to the end that fails leaves the offset in place
	}

	if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
		return extent{}, false, fmt.Errorf("%w: restoring the input's offset: %w", ErrSource, err)
	}

	if end < offset {
		return extent{}, false, nil
	}

	return extent{readerAt: readerAt, offset: offset, size: end - offset}, true, nil
}

// headerAt is the first peek bytes of the extent, fewer when it is shorter;
// a failing read is ErrSource.
func headerAt(ext extent, peek int) ([]byte, error) {
	header := make([]byte, min(int64(peek), ext.size))

	n, err := ext.readerAt.ReadAt(header, ext.offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: %w", ErrSource, err)
	}

	return header[:n], nil
}

// seekableFormat is the registered format whose magic opens header and that
// decodes a seekable input, if there is one. A format the stream path knows
// but this one does not, and an unknown header, are the stream path's to
// serve or report.
func seekableFormat(fmts []format, header []byte) (format, bool) {
	for _, f := range fmts {
		if f.newAt != nil && bytes.HasPrefix(header, f.magic) {
			return f, true
		}
	}

	return format{}, false
}

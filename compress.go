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

// format is a registered decompression format.
type format struct {
	name  string
	magic []byte
	new   Decompressor
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
	formatsMu.Lock()
	defer formatsMu.Unlock()

	formats = append(formats, format{name: name, magic: magic, new: fn})

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
func Decompress(reader io.Reader) (io.ReadCloser, error) {
	formatsMu.RLock()

	fmts := formats
	peek := peekSize

	formatsMu.RUnlock()

	if peek == 0 {
		return nil, fmt.Errorf("%w: no decompression format registered", ErrUnknownFormat)
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

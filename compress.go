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
	Register("zstd", []byte{0x28, 0xB5, 0x2F, 0xFD}, decompressZstd)
}

// Compress returns a reader that yields zstd-compressed data from the input.
// The caller must close the returned ReadCloser to release encoder resources.
func Compress(reader io.Reader) (io.ReadCloser, error) {
	pipeReader, pipeWriter := io.Pipe()

	encoder, err := zstd.NewWriter(pipeWriter)
	if err != nil {
		_ = pipeWriter.Close()

		return nil, fmt.Errorf("zstd encoder: %w", err)
	}

	go func() {
		_, copyErr := io.Copy(encoder, reader)

		closeErr := encoder.Close()

		if copyErr != nil {
			_ = pipeWriter.CloseWithError(copyErr)
		} else {
			_ = pipeWriter.CloseWithError(closeErr)
		}
	}()

	return pipeReader, nil
}

// Decompress auto-detects the compression format from magic bytes and returns
// a streaming decompressed reader. Only formats that have been registered
// (via Register or sub-package init) are recognized: anything else is
// ErrUnknownFormat, an empty stream is io.ErrUnexpectedEOF, and a source
// that fails before its magic is read reports its own error.
// The caller must close the returned ReadCloser to release decoder resources.
func Decompress(reader io.Reader) (io.ReadCloser, error) {
	formatsMu.RLock()

	fmts := formats
	peek := peekSize

	formatsMu.RUnlock()

	if peek == 0 {
		return nil, fmt.Errorf("%w: no decompression format registered", ErrUnknownFormat)
	}

	// Buffer the input to batch syscalls for decompressors that make many small reads.
	buffered := bufio.NewReaderSize(reader, inputBufSize)

	header, err := buffered.Peek(peek)
	if err != nil && len(header) == 0 {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}

		return nil, fmt.Errorf("read magic bytes: %w", err)
	}

	for _, f := range fmts {
		if bytes.HasPrefix(header, f.magic) {
			return f.new(buffered)
		}
	}

	return nil, fmt.Errorf("%w (header: %x)", ErrUnknownFormat, header)
}

func decompressZstd(reader io.Reader) (io.ReadCloser, error) {
	decoder, err := zstd.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("zstd decoder: %w", err)
	}

	return &zstdReader{decoder: decoder}, nil
}

// zstdReader adapts zstd.Decoder to io.ReadCloser.
type zstdReader struct {
	decoder *zstd.Decoder
}

func (r *zstdReader) Read(p []byte) (int, error) {
	return r.decoder.Read(p) //nolint:wrapcheck // Pass-through.
}

func (r *zstdReader) Close() error {
	r.decoder.Close()

	return nil
}

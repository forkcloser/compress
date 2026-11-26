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

package codec

import (
	gobzip2 "compress/bzip2"
	"fmt"
	"io"

	kgzip "github.com/klauspost/compress/gzip"
	kzstd "github.com/klauspost/compress/zstd"
	plz4 "github.com/pierrec/lz4/v4"

	uxz "github.com/forkcloser/go-xz/v2"

	"github.com/forkcloser/go-compress/internal/constants"
)

// The raw constructors: each format's decoder or encoder as its library
// builds it, errors as the library reports them, for Reader and Writer to
// class. The decoders that are serial read ahead of their consumer.

// Gzip is the gzip decoder.
func Gzip(r io.Reader) (io.ReadCloser, error) {
	decoder, err := kgzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip decoder: %w", err)
	}

	return ReadAhead(decoder), nil
}

// GzipWriter is the gzip encoder.
func GzipWriter(w io.Writer) (io.WriteCloser, error) {
	return kgzip.NewWriter(w), nil
}

// Xz is the xz decoder.
func Xz(r io.Reader) (io.ReadCloser, error) {
	decoder, err := uxz.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("xz decoder: %w", err)
	}

	return ReadAhead(io.NopCloser(decoder)), nil
}

// XzAt is the xz decoder over a seekable input of known size: the block
// index at the stream's end lets it decode blocks in parallel, on every
// CPU, so it reads no further ahead of its consumer than it already does.
func XzAt(r io.ReaderAt, size int64) (io.ReadCloser, error) {
	decoder, err := uxz.NewReader(io.NewSectionReader(r, 0, size), uxz.WithWorkers(0))
	if err != nil {
		return nil, fmt.Errorf("xz decoder: %w", err)
	}

	return decoder, nil
}

// XzWriter is the xz encoder.
func XzWriter(w io.Writer) (io.WriteCloser, error) {
	encoder, err := uxz.NewWriter(w)
	if err != nil {
		return nil, fmt.Errorf("xz encoder: %w", err)
	}

	return encoder, nil
}

// Bzip2 is the bzip2 decoder; Go has no encoder.
func Bzip2(r io.Reader) (io.ReadCloser, error) {
	return ReadAhead(io.NopCloser(gobzip2.NewReader(r))), nil
}

// Lz4 is the lz4 decoder, decoding in order and read ahead of its consumer
// as the other serial decoders are. Not on several goroutines, which the
// library offers: a reader left before its end keeps them parked with
// their block buffers, and the library's Reset, which lets them go,
// returns while one of them may still read the source.
func Lz4(r io.Reader) (io.ReadCloser, error) {
	return ReadAhead(io.NopCloser(plz4.NewReader(r))), nil
}

// Lz4Writer is the lz4 encoder.
func Lz4Writer(w io.Writer) (io.WriteCloser, error) {
	return plz4.NewWriter(w), nil
}

// ZstdWindow is the largest window a zstd frame may declare to the commands
// unless --long allows more: the zstd tool's own limit. The decoder
// allocates the window a frame declares before it decodes a byte of it.
const ZstdWindow = 128 * constants.MiB

// ErrZstdWindow is in the chain of the error a frame over the limit is
// refused with.
var ErrZstdWindow = kzstd.ErrWindowSizeExceeded

// Zstd is the zstd decoder for a frame of any window the decoder takes:
// 512 MiB at most.
func Zstd(r io.Reader) (io.ReadCloser, error) {
	return ZstdWithin(kzstd.MaxWindowSize)(r)
}

// ZstdWithin is Zstd for frames whose window is at most window bytes. The
// decoder is read ahead of its consumer though it decodes concurrently on
// its own: its Close and its Read are not safe together, and reading ahead
// is what keeps a Close from another goroutine off a Read in flight.
func ZstdWithin(window uint64) func(io.Reader) (io.ReadCloser, error) {
	return func(r io.Reader) (io.ReadCloser, error) {
		decoder, err := kzstd.NewReader(r, kzstd.WithDecoderMaxWindow(window))
		if err != nil {
			return nil, fmt.Errorf("zstd decoder: %w", err)
		}

		return ReadAhead(&zstdReader{decoder: decoder}), nil
	}
}

// ZstdWriter is the zstd encoder.
func ZstdWriter(w io.Writer) (io.WriteCloser, error) {
	encoder, err := kzstd.NewWriter(w)
	if err != nil {
		return nil, fmt.Errorf("zstd encoder: %w", err)
	}

	return encoder, nil
}

// zstdReader adapts the zstd decoder to io.ReadCloser.
type zstdReader struct {
	decoder *kzstd.Decoder
}

func (r *zstdReader) Read(p []byte) (int, error) {
	return r.decoder.Read(p) //nolint:wrapcheck // the decoder's own error, for Reader to class
}

func (r *zstdReader) Close() error {
	r.decoder.Close()

	return nil
}

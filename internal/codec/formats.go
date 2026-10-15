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

	uxz "github.com/forkcloser/xz"
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

// Lz4 is the lz4 decoder, decoding frame blocks on GOMAXPROCS goroutines.
// Abandoning a partially consumed reader parks those workers and their
// block buffers for good, as the library has no cancellation: a consumer
// that stops short of EOF by design (a tar reader halting at the
// end-of-archive marker) must drain the rest before dropping the reader.
func Lz4(r io.Reader) (io.ReadCloser, error) {
	decoder := plz4.NewReader(r)
	if err := decoder.Apply(plz4.ConcurrencyOption(-1)); err != nil {
		return nil, fmt.Errorf("lz4 concurrency: %w", err)
	}

	return io.NopCloser(decoder), nil
}

// Lz4Writer is the lz4 encoder.
func Lz4Writer(w io.Writer) (io.WriteCloser, error) {
	return plz4.NewWriter(w), nil
}

// Zstd is the zstd decoder, which decodes concurrently on its own.
func Zstd(r io.Reader) (io.ReadCloser, error) {
	decoder, err := kzstd.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("zstd decoder: %w", err)
	}

	return &zstdReader{decoder: decoder}, nil
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

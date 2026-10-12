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

package compress_test

// The contract of the package, from its documentation and its callers:
// Decompress recognizes every registered format by its magic and streams the
// payload back whole, whatever its size; what it cannot recognize is refused
// as ErrUnknownFormat, an empty stream as io.ErrUnexpectedEOF, and a failing
// source as its own error; Compress is the zstd half of that round trip. The
// formats and the sizes are the two axes.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"

	"github.com/forkcloser/xz"

	"github.com/forkcloser/compress"
)

// encoder compresses payload in one format, as that format's own writer does.
type encoder func(tb testing.TB, payload []byte) []byte

// encoders are the formats Decompress must recognize and that a writer exists
// for; bzip2 has none in Go and is checked from a fixture.
func encoders() map[string]encoder {
	return map[string]encoder{
		"zstd": func(tb testing.TB, payload []byte) []byte {
			tb.Helper()

			var buf bytes.Buffer

			writer, err := zstd.NewWriter(&buf)
			if err != nil {
				tb.Fatal(err)
			}

			write(tb, writer, payload)

			return buf.Bytes()
		},
		"gzip": func(tb testing.TB, payload []byte) []byte {
			tb.Helper()

			var buf bytes.Buffer

			write(tb, gzip.NewWriter(&buf), payload)

			return buf.Bytes()
		},
		"lz4": func(tb testing.TB, payload []byte) []byte {
			tb.Helper()

			var buf bytes.Buffer

			write(tb, lz4.NewWriter(&buf), payload)

			return buf.Bytes()
		},
		"xz": func(tb testing.TB, payload []byte) []byte {
			tb.Helper()

			var buf bytes.Buffer

			writer, err := xz.NewWriter(&buf)
			if err != nil {
				tb.Fatal(err)
			}

			write(tb, writer, payload)

			return buf.Bytes()
		},
		"compress": func(tb testing.TB, payload []byte) []byte {
			tb.Helper()

			reader, err := compress.Compress(bytes.NewReader(payload))
			if err != nil {
				tb.Fatal(err)
			}

			data, err := io.ReadAll(reader)
			if err != nil {
				tb.Fatal(err)
			}

			if err := reader.Close(); err != nil {
				tb.Fatal(err)
			}

			return data
		},
	}
}

func write(tb testing.TB, writer io.WriteCloser, payload []byte) {
	tb.Helper()

	if _, err := writer.Write(payload); err != nil {
		tb.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		tb.Fatal(err)
	}
}

// sizes are the payload lengths that bear on a streaming decoder: nothing,
// one byte, the edges of the formats' blocks (4 KiB, 64 KiB), of the input
// buffer Decompress reads through (256 KiB), and a few blocks of lz4's
// largest (4 MiB).
func sizes() []int {
	return []int{
		0, 1, 255, 256,
		4*compress.KiB - 1, 4 * compress.KiB, 4*compress.KiB + 1,
		64*compress.KiB - 1, 64 * compress.KiB, 64*compress.KiB + 1,
		256*compress.KiB - 1, 256 * compress.KiB, 256*compress.KiB + 1,
		4*compress.MiB + 3,
	}
}

// payload is n bytes that compress, but not to nothing: text with a
// byte-level drift, the same for the same n.
func payload(n int) []byte {
	const text = "the quick brown fox jumps over the lazy dog; "

	out := make([]byte, n)
	for i := range out {
		out[i] = text[i%len(text)] ^ byte(i/len(text)%7)
	}

	return out
}

// decompressed runs Decompress over data and drains it.
func decompressed(t *testing.T, data []byte) ([]byte, error) {
	t.Helper()

	reader, err := compress.Decompress(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		_ = reader.Close() // a decoder may report the stream's error again here

		return data, readErr
	}

	return data, reader.Close()
}

// TestBoundedRoundTrip: every format, every size, back whole through
// Decompress.
func TestBoundedRoundTrip(t *testing.T) {
	t.Parallel()

	for name, encode := range encoders() {
		for _, size := range sizes() {
			t.Run(name+"/"+strconv.Itoa(size), func(t *testing.T) {
				t.Parallel()

				want := payload(size)

				got, err := decompressed(t, encode(t, want))
				if err != nil {
					t.Fatal(err)
				}

				if !bytes.Equal(got, want) {
					t.Fatalf("%d bytes back, want %d", len(got), len(want))
				}
			})
		}
	}
}

// TestBzip2Fixture: bzip2, which Go reads but does not write, from a fixture.
func TestBzip2Fixture(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "payload.bz2"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := decompressed(t, data)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != testPayload {
		t.Fatalf("got %q, want %q", got, testPayload)
	}
}

// TestTruncatedStreams: a stream cut short is an error on the read, in every
// format, never a clean EOF with fewer bytes: cut in the middle, and cut at
// the end so that only the trailer is missing.
func TestTruncatedStreams(t *testing.T) {
	t.Parallel()

	const trailer = 8 // gzip's CRC-32 and size; the others' trailers are no shorter

	for name, encode := range encoders() {
		for _, cut := range []string{"middle", "trailer"} {
			t.Run(name+"/"+cut, func(t *testing.T) {
				t.Parallel()

				whole := encode(t, payload(256*compress.KiB+1))

				end := len(whole) / 2
				if cut == "trailer" {
					end = len(whole) - trailer
				}

				got, err := decompressed(t, whole[:end])
				if err == nil {
					t.Fatalf("%s cut at %s: %d bytes read with no error", name, cut, len(got))
				}
			})
		}
	}
}

// TestRefusals: what Decompress refuses, and as what.
func TestRefusals(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input []byte
		want  error
	}{
		"empty":                   {nil, io.ErrUnexpectedEOF},
		"one unknown byte":        {[]byte{0x00}, compress.ErrUnknownFormat},
		"shorter than any magic":  {[]byte{0xAB, 0xCD}, compress.ErrUnknownFormat},
		"unknown, longer":         {bytes.Repeat([]byte{0x01}, 1024), compress.ErrUnknownFormat},
		"zstd magic, three bytes": {[]byte{0x28, 0xB5, 0x2F}, compress.ErrUnknownFormat},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := compress.Decompress(bytes.NewReader(tc.input))
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// TestReaderErrors: a source that fails is the error the caller gets, from
// Decompress when it fails on the magic, from the compressed reader when the
// source fails under Compress.
func TestReaderErrors(t *testing.T) {
	t.Parallel()

	_, err := compress.Decompress(&failingReader{err: errBoom})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Decompress: got %v, want %v", err, errBoom)
	}

	compressed, err := compress.Compress(&failingReader{err: errBoom})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = io.ReadAll(compressed); !errors.Is(err, errBoom) {
		t.Fatalf("Compress read: got %v, want %v", err, errBoom)
	}

	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
}

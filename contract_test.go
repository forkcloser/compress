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
// payload back whole, whatever its size. Every error it or its readers return
// wraps one of four classes, with the cause in the chain: what it cannot
// recognize is ErrUnknownFormat; an empty stream, one its decoder refuses or
// one cut short is ErrCorrupt (io.ErrUnexpectedEOF in the chain for a cut);
// a source that fails, before the magic or under the decoder, is ErrSource
// with the source's own error; a Read after Close is ErrClosed. Compress is
// the zstd half of the round trip, with the same classes. The formats and
// the sizes are the two axes.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"

	"github.com/forkcloser/xz"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/constants"
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

			reader := compress.Compress(bytes.NewReader(payload))

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
		4*constants.KiB - 1, 4 * constants.KiB, 4*constants.KiB + 1,
		64*constants.KiB - 1, 64 * constants.KiB, 64*constants.KiB + 1,
		256*constants.KiB - 1, 256 * constants.KiB, 256*constants.KiB + 1,
		4*constants.MiB + 3,
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

				whole := encode(t, payload(256*constants.KiB+1))

				end := len(whole) / 2
				if cut == "trailer" {
					end = len(whole) - trailer
				}

				got, err := decompressed(t, whole[:end])
				if err == nil {
					t.Fatalf("%s cut at %s: %d bytes read with no error", name, cut, len(got))
				}

				if !errors.Is(err, compress.ErrCorrupt) || errors.Is(err, compress.ErrSource) {
					t.Fatalf("%s cut at %s: %v, want ErrCorrupt and not ErrSource", name, cut, err)
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
		"empty":                   {nil, compress.ErrCorrupt},
		"empty, the cut":          {nil, io.ErrUnexpectedEOF},
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

// TestReaderErrors: a source that fails is ErrSource with the source's own
// error in the chain, from Decompress when it fails on the magic, from the
// compressed reader when the source fails under Compress.
func TestReaderErrors(t *testing.T) {
	t.Parallel()

	_, err := compress.Decompress(&failingReader{err: errBoom})
	if !errors.Is(err, errBoom) || !errors.Is(err, compress.ErrSource) {
		t.Fatalf("Decompress: got %v, want ErrSource wrapping %v", err, errBoom)
	}

	compressed := compress.Compress(&failingReader{err: errBoom})

	if _, err = io.ReadAll(compressed); !errors.Is(err, errBoom) || !errors.Is(err, compress.ErrSource) {
		t.Fatalf("Compress read: got %v, want ErrSource wrapping %v", err, errBoom)
	}

	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
}

// halfThenFail yields the first half of data, then fails with err: a source
// that breaks under the decoder, after the magic was read.
type halfThenFail struct {
	data []byte
	err  error
}

func (r *halfThenFail) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}

	n := copy(p, r.data[:min(len(p), max(1, len(r.data)/2))])
	r.data = r.data[n:]

	return n, nil
}

// TestSourceFailsUnderDecoder: a source that breaks after the magic is
// ErrSource on the read, with the source's error in the chain and never
// ErrCorrupt, whatever the decoder itself made of the cut; the payload is
// past the input buffer so the failure is met by a decoder mid-stream.
func TestSourceFailsUnderDecoder(t *testing.T) {
	t.Parallel()

	for name, encode := range encoders() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			whole := encode(t, payload(2*constants.MiB+1))

			reader, err := compress.Decompress(&halfThenFail{data: whole, err: errBoom})
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = reader.Close() }()

			_, err = io.ReadAll(reader)
			if !errors.Is(err, compress.ErrSource) || !errors.Is(err, errBoom) || errors.Is(err, compress.ErrCorrupt) {
				t.Fatalf("got %v, want ErrSource wrapping %v, not ErrCorrupt", err, errBoom)
			}
		})
	}
}

// TestCorruptBody: a stream whose bytes past the magic are wrong is
// ErrCorrupt, on Decompress or on the read, and never ErrSource: the source
// delivered every byte it had.
func TestCorruptBody(t *testing.T) {
	t.Parallel()

	for name, encode := range encoders() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			whole := encode(t, payload(64*constants.KiB+1))
			for i := 8; i < len(whole); i += 97 {
				whole[i] ^= 0x5A
			}

			_, err := decompressed(t, whole)
			if !errors.Is(err, compress.ErrCorrupt) || errors.Is(err, compress.ErrSource) {
				t.Fatalf("got %v, want ErrCorrupt and not ErrSource", err)
			}
		})
	}
}

// stallingSource yields its data, then parks every Read until released, then
// ends: a source that hangs under the decoder.
type stallingSource struct {
	data    []byte
	release chan struct{}
}

func (s *stallingSource) Read(p []byte) (int, error) {
	if len(s.data) > 0 {
		n := copy(p, s.data)
		s.data = s.data[n:]

		return n, nil
	}

	<-s.release

	return 0, io.EOF
}

// TestCloseWhileParked: a Read parked on a source that hangs comes back
// ErrClosed when another goroutine closes the reader, through Decompress,
// for a format that reads ahead; Close itself returns once the source does.
func TestCloseWhileParked(t *testing.T) {
	t.Parallel()

	whole := encoders()["gzip"](t, payload(4*constants.MiB+1))
	source := &stallingSource{data: whole[:len(whole)-2], release: make(chan struct{})}

	reader, err := compress.Decompress(source)
	if err != nil {
		t.Fatal(err)
	}

	readErr := make(chan error, 1)

	go func() {
		_, err := io.Copy(io.Discard, reader) // drains what was decoded, then parks on the stall
		readErr <- err
	}()

	time.Sleep(50 * time.Millisecond) // the Read is parked

	closeErr := make(chan error, 1)

	go func() { closeErr <- reader.Close() }()

	select {
	case err := <-readErr:
		if !errors.Is(err, compress.ErrClosed) {
			t.Fatalf("the parked Read returned %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the parked Read did not return after Close")
	}

	close(source.release)

	select {
	case err := <-closeErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after the source did")
	}
}

// TestReadAfterClose: a Read on a closed reader is ErrClosed, from
// Decompress's reader and from Compress's.
func TestReadAfterClose(t *testing.T) {
	t.Parallel()

	whole := encoders()["zstd"](t, payload(constants.KiB))

	reader, err := compress.Decompress(bytes.NewReader(whole))
	if err != nil {
		t.Fatal(err)
	}

	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, compress.ErrClosed) {
		t.Fatalf("Decompress reader after Close: got %v, want ErrClosed", err)
	}

	compressed := compress.Compress(bytes.NewReader(payload(constants.MiB)))
	if _, err := compressed.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}

	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := compressed.Read(make([]byte, 1)); !errors.Is(err, compress.ErrClosed) {
		t.Fatalf("Compress reader after Close: got %v, want ErrClosed", err)
	}
}

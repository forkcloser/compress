/*
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

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/forkcloser/xz"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/constants"
)

// Decompress takes the seekable path on its own: an input that is an
// io.ReaderAt and an io.Seeker is decoded from its offset to its end, read at
// offsets and never through Read, and xz, whose index sits at the end, is
// decoded in parallel; an input that only reads stays on the stream path, as
// does a seekable input of a format seekability buys nothing. The failures:
// an input whose Seek overstates its size is a stream that is not what it
// says, ErrCorrupt; one whose ReadAt fails is ErrSource, as is one whose
// Seek cannot restore the offset once the end was measured, since it then
// sits at its end with nothing for either path.

// counted is a seekable input counting the calls each path makes.
type counted struct {
	*bytes.Reader

	reads, readAts atomic.Int64
}

func (c *counted) Read(p []byte) (int, error) {
	c.reads.Add(1)

	return c.Reader.Read(p)
}

func (c *counted) ReadAt(p []byte, off int64) (int, error) {
	c.readAts.Add(1)

	return c.Reader.ReadAt(p, off)
}

// streamOnly hides the seekable methods of a bytes.Reader.
type streamOnly struct{ r *bytes.Reader }

func (s streamOnly) Read(p []byte) (int, error) { return s.r.Read(p) }

// xzBlocks compresses payload as xz in blocks of blockSize, so the parallel
// reader has several to share out.
func xzBlocks(tb testing.TB, payload []byte, blockSize int64) []byte {
	tb.Helper()

	var buf bytes.Buffer

	writer, err := xz.WriterConfig{BlockSize: blockSize}.NewWriter(&buf)
	if err != nil {
		tb.Fatal(err)
	}

	write(tb, writer, payload)

	return buf.Bytes()
}

func drain(tb testing.TB, reader io.ReadCloser) []byte {
	tb.Helper()

	got, err := io.ReadAll(reader)
	if err != nil {
		tb.Fatal(err)
	}

	if err := reader.Close(); err != nil {
		tb.Fatal(err)
	}

	return got
}

func TestSeekableXzTakesTheParallelPath(t *testing.T) {
	t.Parallel()

	want := payload(3*constants.MiB + 7)
	data := xzBlocks(t, want, 256*constants.KiB)

	input := &counted{Reader: bytes.NewReader(data)}

	reader, err := compress.Decompress(input)
	if err != nil {
		t.Fatal(err)
	}

	if got := drain(t, reader); !bytes.Equal(got, want) {
		t.Fatalf("%d bytes back; want %d", len(got), len(want))
	}

	if input.reads.Load() != 0 || input.readAts.Load() == 0 {
		t.Fatalf(
			"Read called %d times, ReadAt %d; want the input read at offsets only",
			input.reads.Load(),
			input.readAts.Load(),
		)
	}

	// The input's offset is where it was.
	if off, err := input.Seek(0, io.SeekCurrent); err != nil || off != 0 {
		t.Fatalf("the input's offset after Decompress: %d, %v; want 0", off, err)
	}
}

func TestSeekableInputFromAnOffset(t *testing.T) {
	t.Parallel()

	want := payload(300 * constants.KiB)
	data := xzBlocks(t, want, 64*constants.KiB)
	junk := []byte("twelve bytes")
	input := bytes.NewReader(slices.Concat(junk, data))

	if _, err := input.Seek(int64(len(junk)), io.SeekStart); err != nil {
		t.Fatal(err)
	}

	reader, err := compress.Decompress(input)
	if err != nil {
		t.Fatal(err)
	}

	if got := drain(t, reader); !bytes.Equal(got, want) {
		t.Fatalf("%d bytes back from the offset; want %d", len(got), len(want))
	}
}

func TestSeekableFile(t *testing.T) {
	t.Parallel()

	want := payload(2 * constants.MiB)
	name := filepath.Join(t.TempDir(), "payload.xz")

	if err := os.WriteFile(name, xzBlocks(t, want, 512*constants.KiB), 0o600); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = file.Close() }()

	reader, err := compress.Decompress(file)
	if err != nil {
		t.Fatal(err)
	}

	if got := drain(t, reader); !bytes.Equal(got, want) {
		t.Fatalf("%d bytes back from the file; want %d", len(got), len(want))
	}
}

func TestStreamInputStaysOnTheStreamPath(t *testing.T) {
	t.Parallel()

	want := payload(300 * constants.KiB)
	data := xzBlocks(t, want, 64*constants.KiB)

	reader, err := compress.Decompress(streamOnly{bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}

	if got := drain(t, reader); !bytes.Equal(got, want) {
		t.Fatalf("%d bytes back through the stream; want %d", len(got), len(want))
	}
}

func TestSeekableInputOfAStreamFormatStaysOnTheStreamPath(t *testing.T) {
	t.Parallel()

	want := payload(300 * constants.KiB)

	for name, encode := range encoders() {
		if name == "xz" || name == "compress" {
			continue
		}

		input := &counted{Reader: bytes.NewReader(encode(t, want))}

		reader, err := compress.Decompress(input)
		if err != nil {
			t.Fatal(err)
		}

		if got := drain(t, reader); !bytes.Equal(got, want) {
			t.Fatalf("%s: %d bytes back; want %d", name, len(got), len(want))
		}

		if input.reads.Load() == 0 {
			t.Fatalf("%s: a seekable input of a stream format was not read through Read", name)
		}
	}
}

// overstated is a seekable input whose Seek reports more bytes than its
// ReadAt can serve.
type overstated struct {
	*bytes.Reader

	extra int64
}

func (o *overstated) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekEnd {
		return o.Size() + o.extra, nil
	}

	return o.Reader.Seek(offset, whence)
}

func TestSeekableSizeThatLies(t *testing.T) {
	t.Parallel()

	data := xzBlocks(t, payload(300*constants.KiB), 64*constants.KiB)
	input := &overstated{Reader: bytes.NewReader(data), extra: 4096}

	reader, err := compress.Decompress(input)
	if err == nil {
		_, err = io.ReadAll(reader)
		_ = reader.Close()
	}

	if !errors.Is(err, compress.ErrCorrupt) {
		t.Fatalf("an input overstating its size: %v; want ErrCorrupt", err)
	}
}

// failingAt is a seekable input whose ReadAt fails at or past off.
type failingAt struct {
	*bytes.Reader

	off int64
	err error
}

func (f *failingAt) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > f.off {
		return 0, f.err
	}

	return f.Reader.ReadAt(p, off)
}

var errDisk = errors.New("disk")

func TestSeekableInputThatFails(t *testing.T) {
	t.Parallel()

	data := xzBlocks(t, payload(300*constants.KiB), 64*constants.KiB)

	// Before the magic: ErrSource at Decompress. Under the decoder: ErrSource
	// at Decompress (the index is read first) or on a Read.
	for name, at := range map[string]int64{"at the magic": 0, "under the decoder": int64(len(data) / 2)} {
		input := &failingAt{Reader: bytes.NewReader(data), off: at, err: errDisk}

		reader, err := compress.Decompress(input)
		if err == nil {
			_, err = io.ReadAll(reader)
			_ = reader.Close()
		}

		if !errors.Is(err, compress.ErrSource) || !errors.Is(err, errDisk) {
			t.Fatalf("%s: %v; want ErrSource with the input's error", name, err)
		}
	}
}

// unrestorable is a seekable input whose Seek back to a position fails,
// after a Seek to the end succeeded.
type unrestorable struct {
	*bytes.Reader

	err error
}

func (u *unrestorable) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekStart {
		return 0, u.err
	}

	return u.Reader.Seek(offset, whence)
}

var errSeek = errors.New("seek")

func TestSeekableInputThatCannotRestoreItsOffset(t *testing.T) {
	t.Parallel()

	data := xzBlocks(t, payload(300*constants.KiB), 64*constants.KiB)

	reader, err := compress.Decompress(&unrestorable{Reader: bytes.NewReader(data), err: errSeek})
	if reader != nil || !errors.Is(err, compress.ErrSource) || !errors.Is(err, errSeek) {
		t.Fatalf(
			"an input that cannot restore its offset: %v, %v; want no reader and ErrSource with the Seek's error",
			reader,
			err,
		)
	}
}

func TestSeekableReadAfterClose(t *testing.T) {
	t.Parallel()

	data := xzBlocks(t, payload(300*constants.KiB), 64*constants.KiB)

	reader, err := compress.Decompress(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, compress.ErrClosed) {
		t.Fatalf("Read after Close: %v; want ErrClosed", err)
	}
}

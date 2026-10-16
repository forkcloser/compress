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

import (
	"bytes"
	"io"
	"testing"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/constants"
)

// The guards: the package exists to stream blob-scale content through these
// decoders; a change that keeps every test green and halves the throughput
// shows here.

func BenchmarkDecompress(b *testing.B) {
	want := payload(8 * constants.MiB)

	for name, encode := range encoders() {
		if name == "compress" {
			continue
		}

		data := encode(b, want)

		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(want)))

			for b.Loop() {
				reader, err := compress.Decompress(bytes.NewReader(data))
				if err != nil {
					b.Fatal(err)
				}

				if _, err := io.Copy(io.Discard, reader); err != nil {
					b.Fatal(err)
				}

				_ = reader.Close()
			}
		})
	}
}

func BenchmarkCompress(b *testing.B) {
	want := payload(8 * constants.MiB)

	b.SetBytes(int64(len(want)))

	for b.Loop() {
		reader := compress.Compress(bytes.NewReader(want))

		if _, err := io.Copy(io.Discard, reader); err != nil {
			b.Fatal(err)
		}

		_ = reader.Close()
	}
}

// BenchmarkDecompressXz: the same multi-block xz through the stream path and
// the seekable one, the before and after of taking the parallel decoder on a
// seekable input.
func BenchmarkDecompressXz(b *testing.B) {
	want := payload(64 * constants.MiB)
	data := xzBlocks(b, want, constants.MiB)

	for name, input := range map[string]func() io.Reader{
		"stream":   func() io.Reader { return streamOnly{bytes.NewReader(data)} },
		"seekable": func() io.Reader { return bytes.NewReader(data) },
	} {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(want)))

			for b.Loop() {
				reader, err := compress.Decompress(input())
				if err != nil {
					b.Fatal(err)
				}

				if _, err := io.Copy(io.Discard, reader); err != nil {
					b.Fatal(err)
				}

				_ = reader.Close()
			}
		})
	}
}

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

// The example the properties cannot state: lz4's skippable frame after the
// data, which a blob store appends as sector padding.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/pierrec/lz4/v4"

	"github.com/forkcloser/compress"
	_ "github.com/forkcloser/compress/bzip2"
	_ "github.com/forkcloser/compress/gzip"
	_ "github.com/forkcloser/compress/lz4"
	_ "github.com/forkcloser/compress/xz"
)

var errBoom = errors.New("boom")

const testPayload = "the quick brown fox jumps over the lazy dog"

type failingReader struct {
	err error
}

func (r *failingReader) Read([]byte) (int, error) {
	return 0, r.err
}

// TestDecompress_LZ4ParallelMultiBlock exercises the concurrent decode path
// the lz4 subpackage enables: a payload spanning many independent blocks,
// followed by a skippable frame (the sector padding a blob store appends),
// must round-trip exactly and reach clean EOF.
func TestDecompress_LZ4ParallelMultiBlock(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("parallel lz4 block decode round-trip "), 200000)

	var buf bytes.Buffer

	writer := lz4.NewWriter(&buf)
	if err := writer.Apply(lz4.BlockSizeOption(lz4.Block64Kb), lz4.ConcurrencyOption(-1)); err != nil {
		t.Fatalf("writer options: %v", err)
	}

	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("compress: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	// Trailing skippable frame: magic 0x184D2A50 + LE length + zeros.
	tail := make([]byte, 512)
	binary.LittleEndian.PutUint32(tail[0:4], 0x184D2A50)
	binary.LittleEndian.PutUint32(tail[4:8], uint32(len(tail)-8))
	buf.Write(tail)

	decompressed, err := compress.Decompress(&buf)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}

	defer func() { _ = decompressed.Close() }()

	got, err := io.ReadAll(decompressed)
	if err != nil {
		t.Fatalf("parallel decode failed (or the skippable tail broke it): %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

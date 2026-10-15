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
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/forkcloser/compress"
)

// FuzzDecompress: whatever the bytes, Decompress refuses them or hands back a
// reader that is drained to an error or EOF; nothing panics, nothing hangs.
func FuzzDecompress(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x28, 0xB5, 0x2F, 0xFD})       // zstd magic, nothing after
	f.Add([]byte{0x1F, 0x8B})                   // gzip
	f.Add([]byte{0x04, 0x22, 0x4D, 0x18})       // lz4
	f.Add([]byte{0xFD, 0x37, 0x7A, 0x58, 0x5A}) // xz
	f.Add([]byte{0x42, 0x5A, 0x68})             // bzip2
	f.Add(make([]byte, 300*1024))               // past the input buffer, no magic

	f.Fuzz(func(_ *testing.T, data []byte) {
		reader, err := compress.Decompress(bytes.NewReader(data))
		if err != nil {
			return
		}

		_, _ = io.ReadAll(reader)
		_ = reader.Close()
	})
}

// FuzzUntarName: whatever an entry is named, Untar writes inside the root or
// refuses the entry; nothing lands beside the root and nothing panics.
func FuzzUntarName(f *testing.F) {
	for _, seed := range []string{"a.txt", "./a.txt", "./", ".", "", "../a", "a/../../b", "/etc/passwd", "a//b", "a/./b", `..\a`} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		var buf bytes.Buffer

		writer := tar.NewWriter(&buf)
		if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: 1}); err != nil {
			return // a name the tar format itself cannot carry
		}

		_, _ = writer.Write([]byte("x"))
		_ = writer.Close()

		parent := t.TempDir()
		dest := filepath.Join(parent, "dest")

		if err := os.Mkdir(dest, 0o755); err != nil {
			t.Fatal(err)
		}

		root, err := os.OpenRoot(dest)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = root.Close() }()

		if err := compress.Untar(&buf, root); err != nil && !errors.Is(err, compress.ErrDestination) {
			t.Fatalf("Untar(%q): unexpected error class: %v", name, err)
		}

		entries, readErr := os.ReadDir(parent)
		if readErr != nil {
			t.Fatal(readErr)
		}

		for _, entry := range entries {
			if entry.Name() != "dest" {
				t.Fatalf("Untar(%q) wrote %q beside the root", name, entry.Name())
			}
		}
	})
}

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
	"slices"
	"testing"

	"github.com/forkcloser/compress"
)

const testDirPerm = 0o700

// writeFile creates the file at path with content, creating no directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile reads the file at path.
func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestTar(t *testing.T) {
	t.Parallel()

	// Create source directory structure.
	baseDir := t.TempDir()
	relDir := "mydir"
	srcDir := filepath.Join(baseDir, relDir)

	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), testDirPerm); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(srcDir, "hello.txt"), "hello")
	writeFile(t, filepath.Join(srcDir, "sub", "world.txt"), "world")

	// Create tar and read all output.
	tarData, err := io.ReadAll(compress.Tar(baseDir, relDir))
	if err != nil {
		t.Fatalf("Tar: %v", err)
	}

	// Read tar and verify entries.
	tarReader := tar.NewReader(bytes.NewReader(tarData))
	entries := make(map[string]string)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}

		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tarReader)
			if err != nil {
				t.Fatalf("read %s: %v", header.Name, err)
			}

			entries[header.Name] = string(data)
		}
	}

	if got, ok := entries["mydir/hello.txt"]; !ok || got != "hello" {
		t.Errorf("hello.txt: got %q, want %q", got, "hello")
	}

	if got, ok := entries["mydir/sub/world.txt"]; !ok || got != "world" {
		t.Errorf("sub/world.txt: got %q, want %q", got, "world")
	}
}

func TestUntar(t *testing.T) {
	t.Parallel()

	// Build a tar archive in memory.
	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	writeEntry := func(name, content string) {
		t.Helper()

		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader %s: %v", name, err)
		}

		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("Write %s: %v", name, err)
		}
	}

	writeEntry("file1.txt", "alpha")
	writeEntry("subdir/file2.txt", "beta")

	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}

	// Extract.
	destDir := t.TempDir()
	if err := compress.Untar(&buf, destDir); err != nil {
		t.Fatalf("Untar: %v", err)
	}

	// Verify.
	if got := readFile(t, filepath.Join(destDir, "file1.txt")); got != "alpha" {
		t.Errorf("file1.txt: got %q, want %q", got, "alpha")
	}

	if got := readFile(t, filepath.Join(destDir, "subdir", "file2.txt")); got != "beta" {
		t.Errorf("subdir/file2.txt: got %q, want %q", got, "beta")
	}
}

func TestUntar_PathTraversal(t *testing.T) {
	t.Parallel()

	// Build a tar with a path traversal entry.
	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	header := &tar.Header{
		Name: "../escape.txt",
		Mode: 0o644,
		Size: 4,
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatal(err)
	}

	if _, writeErr := tarWriter.Write([]byte("evil")); writeErr != nil {
		t.Fatal(writeErr)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	err := compress.Untar(&buf, destDir)

	if !errors.Is(err, compress.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestTar_Roundtrip(t *testing.T) {
	t.Parallel()

	// Create source.
	baseDir := t.TempDir()
	relDir := "data"
	srcDir := filepath.Join(baseDir, relDir)

	if err := os.MkdirAll(srcDir, testDirPerm); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(srcDir, "test.bin"), "roundtrip-content")

	// Tar → Untar roundtrip.
	destDir := t.TempDir()
	if err := compress.Untar(compress.Tar(baseDir, relDir), destDir); err != nil {
		t.Fatalf("Tar→Untar: %v", err)
	}

	// Verify.
	if got := readFile(t, filepath.Join(destDir, "data", "test.bin")); got != "roundtrip-content" {
		t.Errorf("got %q, want %q", got, "roundtrip-content")
	}
}

func TestUntar_EmptyArchive(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()

	if err := compress.Untar(&buf, destDir); err != nil {
		t.Fatalf("Untar empty archive: %v", err)
	}
}

func TestTar_NonexistentSource(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	rc := compress.Tar(baseDir, "does-not-exist")

	defer func() {
		if err := rc.Close(); err != nil {
			t.Error(err)
		}
	}()

	if _, err := io.ReadAll(rc); err == nil {
		t.Fatal("expected error for nonexistent source directory")
	}
}

// TestTar_LeavesSymlinksOut: a symlink in the source is not an entry, and
// does not break the archive; the regular file beside it is archived.
func TestTar_LeavesSymlinksOut(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	srcDir := filepath.Join(baseDir, "src")

	if err := os.MkdirAll(srcDir, testDirPerm); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(srcDir, "file.txt"), "content")

	if err := os.Symlink(filepath.Join(srcDir, "file.txt"), filepath.Join(srcDir, "link")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	names := entryNames(t, compress.Tar(baseDir, "src"))

	if want := []string{"src", "src/file.txt"}; !slices.Equal(names, want) {
		t.Fatalf("entries %q, want %q", names, want)
	}
}

// TestTar_LinkedRoot: a relDir that is a symlink to a directory archives that
// directory, under relDir's name.
func TestTar_LinkedRoot(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	realDir := filepath.Join(t.TempDir(), "real")

	if err := os.MkdirAll(realDir, testDirPerm); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(realDir, "file.txt"), "content")

	if err := os.Symlink(realDir, filepath.Join(baseDir, "link")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	names := entryNames(t, compress.Tar(baseDir, "link"))

	if want := []string{"link", "link/file.txt"}; !slices.Equal(names, want) {
		t.Fatalf("entries %q, want %q", names, want)
	}
}

// TestUntar_RootEntry: "./" and the names under it, as most archives are laid
// out, extract into destDir; the root entry is destDir itself, not an escape.
func TestUntar_RootEntry(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	if err := tarWriter.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}

	if err := tarWriter.WriteHeader(
		&tar.Header{Name: "./a.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
	); err != nil {
		t.Fatal(err)
	}

	if _, err := tarWriter.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	if err := compress.Untar(&buf, destDir); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(destDir, "a.txt")); got != "x" {
		t.Fatalf("got %q, want %q", got, "x")
	}
}

// TestTar_RoundtripWholeBase: Tar(baseDir, "") archives baseDir itself, whose
// root entry is ".", and Untar takes it.
func TestTar_RoundtripWholeBase(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	writeFile(t, filepath.Join(baseDir, "test.bin"), "whole")

	destDir := t.TempDir()
	if err := compress.Untar(compress.Tar(baseDir, ""), destDir); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, filepath.Join(destDir, "test.bin")); got != "whole" {
		t.Fatalf("got %q, want %q", got, "whole")
	}
}

// entryNames reads the archive and returns its entry names, in order.
func entryNames(t *testing.T, archive io.ReadCloser) []string {
	t.Helper()

	defer func() {
		if err := archive.Close(); err != nil {
			t.Error(err)
		}
	}()

	tarReader := tar.NewReader(archive)

	var names []string

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return names
		}

		if err != nil {
			t.Fatal(err)
		}

		names = append(names, header.Name)
	}
}

func TestTar_EmptyDirectory(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	relDir := "empty"

	if err := os.MkdirAll(filepath.Join(baseDir, relDir), testDirPerm); err != nil {
		t.Fatal(err)
	}

	tarData, err := io.ReadAll(compress.Tar(baseDir, relDir))
	if err != nil {
		t.Fatalf("Tar: %v", err)
	}

	// Should produce a valid archive with just the directory entry.
	tarReader := tar.NewReader(bytes.NewReader(tarData))

	var count int

	for {
		_, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}

		count++
	}

	if count == 0 {
		t.Error("expected at least the directory entry in the archive")
	}
}

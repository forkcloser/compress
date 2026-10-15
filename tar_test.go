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
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/compress"
)

// tree is the fs.FS the Tar tests archive: a file at the root, a directory
// with a file, an empty directory, and a symlink.
func tree() fstest.MapFS {
	return fstest.MapFS{
		"a.txt":        {Data: []byte("alpha")},
		"dir/b.txt":    {Data: []byte("beta")},
		"empty":        {Mode: fs.ModeDir | 0o755},
		"link":         {Data: []byte("a.txt"), Mode: fs.ModeSymlink | 0o777},
		"dir/sub/c.go": {Data: []byte("package c")},
	}
}

// entries reads every header of an archive.
func entries(t *testing.T, archive io.Reader) []*tar.Header {
	t.Helper()

	var headers []*tar.Header

	r := tar.NewReader(archive)

	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return headers
		}

		if err != nil {
			t.Fatal(err)
		}

		headers = append(headers, h)
	}
}

// TestTar: the archive holds every entry of the tree as archive/tar writes
// them: directories with a trailing slash, files with their content,
// symlinks with their target, in walk order.
func TestTar(t *testing.T) {
	t.Parallel()

	archive := compress.Tar(tree())

	data, err := io.ReadAll(archive)
	if err != nil {
		t.Fatal(err)
	}

	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}

	var names []string

	for _, h := range entries(t, bytes.NewReader(data)) {
		names = append(names, h.Name)

		switch h.Name {
		case "a.txt", "dir/b.txt", "dir/sub/c.go":
			if h.Typeflag != tar.TypeReg {
				t.Errorf("%s: type %c, want a regular file", h.Name, h.Typeflag)
			}
		case "dir/", "dir/sub/", "empty/":
			if h.Typeflag != tar.TypeDir {
				t.Errorf("%s: type %c, want a directory", h.Name, h.Typeflag)
			}
		case "link":
			if h.Typeflag != tar.TypeSymlink || h.Linkname != "a.txt" {
				t.Errorf("%s: type %c target %q, want a symlink to a.txt", h.Name, h.Typeflag, h.Linkname)
			}
		default:
			t.Errorf("unexpected entry %q", h.Name)
		}
	}

	want := []string{"a.txt", "dir/", "dir/b.txt", "dir/sub/", "dir/sub/c.go", "empty/", "link"}
	if !slices.Equal(names, want) {
		t.Fatalf("entries %q, want %q", names, want)
	}
}

// TestTarIrregularFile: a file AddFS refuses fails the archive as ErrSource,
// reported by Read.
func TestTarIrregularFile(t *testing.T) {
	t.Parallel()

	fsys := tree()
	fsys["fifo"] = &fstest.MapFile{Mode: fs.ModeNamedPipe | 0o644}

	archive := compress.Tar(fsys)

	_, err := io.ReadAll(archive)
	if !errors.Is(err, compress.ErrSource) {
		t.Fatalf("%v; want ErrSource", err)
	}

	_ = archive.Close()
}

// shortFile is a file that announces more bytes than it holds, a file
// shrinking under the walk.
type shortFile struct {
	fstest.MapFS
}

func (s shortFile) Open(name string) (fs.File, error) {
	f, err := s.MapFS.Open(name)
	if err != nil {
		return nil, err
	}

	if name != "short" {
		return f, nil
	}

	return &shortOpen{File: f}, nil
}

// shortOpen reads half of what Stat announces.
type shortOpen struct {
	fs.File

	read int
}

func (s *shortOpen) Read(p []byte) (int, error) {
	if s.read >= 5 {
		return 0, io.EOF
	}

	if len(p) > 5-s.read {
		p = p[:5-s.read]
	}

	n, err := s.File.Read(p)
	s.read += n

	return n, err
}

// TestTarShortFile: a file shorter than its directory entry announced is
// the tree's fault, ErrSource, whether it is the last entry (caught by
// the archive's Close) or not.
func TestTarShortFile(t *testing.T) {
	t.Parallel()

	for _, last := range []bool{true, false} {
		fsys := shortFile{MapFS: fstest.MapFS{"short": {Data: []byte("0123456789")}}}
		if !last {
			fsys.MapFS["z.txt"] = &fstest.MapFile{Data: []byte("after")}
		}

		archive := compress.Tar(fsys)

		_, err := io.ReadAll(archive)
		if !errors.Is(err, compress.ErrSource) {
			t.Errorf("last %v: %v; want ErrSource", last, err)
		}

		_ = archive.Close()
	}
}

// TestTarClosedEarly: a consumer that closes the archive before its end
// leaves nothing behind, and a read after that is ErrClosed.
func TestTarClosedEarly(t *testing.T) {
	t.Parallel()

	archive := compress.Tar(tree())

	if _, err := archive.Read(make([]byte, 10)); err != nil {
		t.Fatal(err)
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := archive.Read(make([]byte, 10)); !errors.Is(err, compress.ErrClosed) {
		t.Fatalf("read after Close: %v; want ErrClosed", err)
	}
}

// openRoot is a temporary directory as an *os.Root, closed with the test.
func openRoot(t *testing.T) (*os.Root, string) {
	t.Helper()

	dir := t.TempDir()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return root, dir
}

// archiveOf writes the given entries into a tar archive.
func archiveOf(t *testing.T, write func(w *tar.Writer)) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := tar.NewWriter(&buf)
	write(w)

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func writeEntry(t *testing.T, w *tar.Writer, h *tar.Header, content string) {
	t.Helper()

	h.Size = int64(len(content))

	if err := w.WriteHeader(h); err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}

// TestUntar: directories and files land under the root with the module's
// permissions, a file's parents are made whether or not the archive names
// them, the "./" root entry is the root itself, and a symlink entry is
// skipped.
func TestUntar(t *testing.T) {
	t.Parallel()

	root, dir := openRoot(t)

	archive := archiveOf(t, func(w *tar.Writer) {
		writeEntry(t, w, &tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o700}, "")
		writeEntry(t, w, &tar.Header{Name: "a.txt", Typeflag: tar.TypeReg, Mode: 0o600}, "alpha")
		writeEntry(t, w, &tar.Header{Name: "d/", Typeflag: tar.TypeDir, Mode: 0o700}, "")
		writeEntry(t, w, &tar.Header{Name: "./d/b.txt", Typeflag: tar.TypeReg, Mode: 0o755}, "beta")
		writeEntry(t, w, &tar.Header{Name: "deep/er/c.txt", Typeflag: tar.TypeReg, Mode: 0o644}, "gamma")
		writeEntry(t, w, &tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "a.txt"}, "")
	})

	if err := compress.Untar(bytes.NewReader(archive), root); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{"a.txt": "alpha", "d/b.txt": "beta", "deep/er/c.txt": "gamma"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}

	if _, err := os.Lstat(filepath.Join(dir, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the symlink entry was extracted: %v", err)
	}

	if runtime.GOOS != "windows" {
		for name, want := range map[string]fs.FileMode{"d/b.txt": 0o644, "d": 0o755} {
			info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}

			if info.Mode().Perm() != want {
				t.Errorf("%s: mode %v; want the module's %v, not the archive's", name, info.Mode().Perm(), want)
			}
		}
	}

	// Every file Untar wrote is closed: it can be removed at once, which on
	// Windows a handle still open would refuse.
	for _, name := range []string{"a.txt", "d/b.txt", "deep/er/c.txt"} {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Errorf("removing %s just after Untar: %v", name, err)
		}
	}
}

// TestUntarEscapes: an entry named to land outside the root, by "..", an
// absolute name, or a symlink in the tree pointing out, is refused as
// ErrDestination and nothing lands beside the root.
func TestUntarEscapes(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	dir := filepath.Join(parent, "dest")

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = root.Close() }()

	names := []string{"../escape.txt", "a/../../escape.txt", "/escape.txt"}

	if runtime.GOOS != "windows" {
		if err = os.Symlink(parent, filepath.Join(dir, "out")); err != nil {
			t.Fatal(err)
		}

		names = append(names, "out/escape.txt")
	}

	for _, name := range names {
		archive := archiveOf(t, func(w *tar.Writer) {
			writeEntry(t, w, &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}, "x")
		})

		if err = compress.Untar(bytes.NewReader(archive), root); !errors.Is(err, compress.ErrDestination) {
			t.Errorf("%q: %v; want ErrDestination", name, err)
		}
	}

	beside, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range beside {
		if e.Name() != "dest" {
			t.Errorf("%q landed beside the root", e.Name())
		}
	}
}

// TestUntarErrors: an archive that does not parse or ends inside an entry
// is ErrCorrupt, a reader that fails is ErrSource, neither leaves a partial
// file, and an empty archive extracts nothing.
func TestUntarErrors(t *testing.T) {
	t.Parallel()

	root, dir := openRoot(t)

	if err := compress.Untar(
		bytes.NewReader([]byte("this is not a tar archive, not at all, no")),
		root,
	); !errors.Is(
		err,
		compress.ErrCorrupt,
	) {
		t.Errorf("garbage: %v; want ErrCorrupt", err)
	}

	archive := archiveOf(t, func(w *tar.Writer) {
		writeEntry(t, w, &tar.Header{Name: "a.txt", Typeflag: tar.TypeReg, Mode: 0o644}, "alpha")
	})
	// Cut inside the entry's content: the file is created, its content
	// fails, and the partial file must go.
	failing := io.MultiReader(bytes.NewReader(archive[:514]), &failingReader{err: errBoom})

	if err := compress.Untar(failing, root); !errors.Is(err, compress.ErrSource) {
		t.Errorf("failing reader: %v; want ErrSource", err)
	}

	// The archive cut inside the entry's content, with no reader failure:
	// the archive is corrupt, and the partial file goes.
	if err := compress.Untar(bytes.NewReader(archive[:514]), root); !errors.Is(err, compress.ErrCorrupt) {
		t.Errorf("cut archive: %v; want ErrCorrupt", err)
	}

	if err := compress.Untar(bytes.NewReader(archiveOf(t, func(*tar.Writer) {})), root); err != nil {
		t.Errorf("empty archive: %v", err)
	}

	if err := compress.Untar(bytes.NewReader(nil), root); err != nil {
		t.Errorf("no bytes: %v", err)
	}

	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(left) != 0 {
		t.Errorf("%d entries left by failed extractions", len(left))
	}
}

// TestTarUntarRoundTrip: a directory archived with Tar and extracted with
// Untar into another root comes back file for file.
func TestTarUntarRoundTrip(t *testing.T) {
	t.Parallel()

	src := t.TempDir()

	for name, content := range map[string]string{"a.txt": "alpha", "d/b.txt": "beta", "d/e/c.txt": "gamma"} {
		p := filepath.Join(src, filepath.FromSlash(name))

		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Mkdir(filepath.Join(src, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	root, dir := openRoot(t)

	archive := compress.Tar(os.DirFS(src))
	if err := compress.Untar(archive, root); err != nil {
		t.Fatal(err)
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{"a.txt": "alpha", "d/b.txt": "beta", "d/e/c.txt": "gamma"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", name, got, err)
		}
	}

	if info, err := os.Stat(filepath.Join(dir, "empty")); err != nil || !info.IsDir() {
		t.Errorf("the empty directory: %v", err)
	}
}

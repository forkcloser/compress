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

package compress

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"

	"github.com/forkcloser/compress/internal/codec"
	"github.com/forkcloser/compress/internal/constants"
)

// Tar returns a reader that yields a tar archive of fsys, as archive/tar's
// Writer.AddFS writes the tree: every directory, regular file and symlink,
// in walk order, with names relative to the root of fsys. A file of any
// other kind fails the archive, as AddFS refuses it; a caller with such
// files in the tree passes a filtered fs.FS. A failure to read the tree is
// ErrSource; a Read after Close is ErrClosed. The caller must close the
// returned ReadCloser.
func Tar(fsys fs.FS) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		_ = pipeWriter.CloseWithError(writeTar(pipeWriter, fsys))
	}()

	return &compressed{reader: pipeReader}
}

// writeTar archives fsys into w and names what ended it: the tree's failure
// to be read (ErrSource) or the consumer closing the pipe under the writer
// (ErrClosed); nil for a clean end.
func writeTar(w io.Writer, fsys fs.FS) error {
	destination := &trackedWriter{writer: w}
	tarWriter := tar.NewWriter(destination)

	err := tarWriter.AddFS(fsys)
	if err == nil {
		// The last entry's shortfall, a file shorter than its directory
		// entry announced, surfaces here: the tree's fault, as a read
		// failure is, unless the pipe itself failed.
		err = tarWriter.Close()
	}

	switch {
	case err == nil:
		return nil
	case destination.err != nil:
		return fmt.Errorf("%w: %w", ErrClosed, err)
	default:
		return fmt.Errorf("%w: %w", ErrSource, err)
	}
}

// trackedWriter remembers whether the writer under the archive failed, so
// an error out of AddFS can be told apart from the tree's.
type trackedWriter struct {
	writer io.Writer
	err    error
}

func (t *trackedWriter) Write(p []byte) (int, error) {
	n, err := t.writer.Write(p)
	if err != nil && t.err == nil {
		t.err = err
	}

	return n, err //nolint:wrapcheck // the pipe's own error, named by writeTar
}

// Untar extracts a tar archive from reader into root: its directories, with
// the module's directory permissions, and its regular files, with the
// module's file permissions; the archive's own modes are not applied, and
// every other kind of entry, symlinks included, is skipped. Every create
// goes through root, which confines it to the root's directory at the OS
// level: an entry named to land outside, by "..", an absolute name or a
// symlink in the tree, is refused by root, and that refusal, like any
// failure to create, is ErrDestination with root's error in the chain. An
// archive that does not parse is ErrCorrupt; a reader that fails is
// ErrSource.
func Untar(reader io.Reader, root *os.Root) error {
	source := codec.Track(reader)
	tarReader := tar.NewReader(source)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return source.Classify(err)
		}

		if err := untarEntry(tarReader, source, header, root); err != nil {
			return err
		}
	}
}

// untarEntry extracts the entry header describes, whose content tarReader is
// positioned at. Names are slash-separated and may start with "./" or be
// "./" itself, the root entry most archives open with.
func untarEntry(tarReader *tar.Reader, source *codec.Source, header *tar.Header, root *os.Root) error {
	name := path.Clean(header.Name)

	switch header.Typeflag {
	case tar.TypeDir:
		if err := root.MkdirAll(name, constants.DirPermissions); err != nil {
			return fmt.Errorf("%w: mkdir %s: %w", ErrDestination, header.Name, err)
		}
	case tar.TypeReg:
		if dir := path.Dir(name); dir != "." {
			if err := root.MkdirAll(dir, constants.DirPermissions); err != nil {
				return fmt.Errorf("%w: mkdir %s: %w", ErrDestination, dir, err)
			}
		}

		if err := extractFile(root, name, tarReader, source); err != nil {
			return fmt.Errorf("write %s: %w", header.Name, err)
		}
	default:
		// Symlinks, devices, pipes: skipped.
	}

	return nil
}

// extractFile writes the entry's content to name under root. A file whose
// content could not be written whole is removed, so a failed extraction
// leaves no partial file that a later run would take for the entry.
func extractFile(root *os.Root, name string, content io.Reader, source *codec.Source) error {
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, constants.FilePermissions)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDestination, err)
	}

	file := &trackedWriter{writer: out}

	if _, err := io.Copy(file, content); err != nil {
		_ = out.Close()
		_ = root.Remove(name)

		// io.Copy's error is the file's when it recorded one; otherwise the
		// content's: the source failed, or the archive ends inside the
		// entry, which the source tracker names ErrSource or ErrCorrupt.
		if file.err != nil {
			return fmt.Errorf("%w: %w", ErrDestination, err)
		}

		return source.Classify(err)
	}

	if err := out.Close(); err != nil {
		_ = root.Remove(name)

		return fmt.Errorf("%w: %w", ErrDestination, err)
	}

	return nil
}

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
	"os"
	"path/filepath"
	"strings"
)

const (
	dirPermissions  os.FileMode = 0o755
	filePermissions os.FileMode = 0o644
)

// Tar returns a reader that yields a tar archive of baseDir/relDir.
// Archive entries are rooted at relDir, preserving the directory structure
// relative to baseDir. Regular files and directories are archived, as Untar
// extracts; a symlink, a device or a pipe is left out, except that relDir
// itself may be a symlink to the directory to archive. The caller must close
// the returned ReadCloser.
func Tar(baseDir, relDir string) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		_ = pipeWriter.CloseWithError(writeTar(pipeWriter, baseDir, relDir))
	}()

	return pipeReader
}

func writeTar(writer io.Writer, baseDir, relDir string) error {
	// The root is resolved: Walk lstats it, and a root that is a symlink would
	// be one entry the filter below leaves out, an empty archive in silence.
	root, err := filepath.EvalSymlinks(filepath.Join(baseDir, relDir))
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}

	tarWriter := tar.NewWriter(writer)

	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		return writeTarEntry(tarWriter, root, relDir, path, info)
	})
	if err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	if err = tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}

	return nil
}

// writeTarEntry writes one walked path: its header, named relDir plus the
// path under root, and for a regular file its content. Anything else is left
// out: FileInfoHeader would describe a symlink as one with no target and no
// size, and Open would then copy the target's content into that entry.
func writeTarEntry(tarWriter *tar.Writer, root, relDir, path string, info os.FileInfo) error {
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil
	}

	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("rel path: %w", err)
	}

	// Tar entries use forward slashes.
	relPath = filepath.ToSlash(filepath.Join(relDir, relPath))

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("tar header: %w", err)
	}

	header.Name = relPath

	if err = tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	if info.IsDir() {
		return nil
	}

	file, err := os.Open(path) // #nosec G304 -- a path under the directory the caller named
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}

	defer func() { _ = file.Close() }() // read-only: a close error carries nothing

	if _, err = io.Copy(tarWriter, file); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}

	return nil
}

// Untar extracts a tar archive from reader into destDir.
// Only regular files and directories are extracted; other entry types
// (symlinks, etc.) are skipped. Path traversal attempts are rejected.
func Untar(reader io.Reader, destDir string) error {
	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		if err := untarEntry(tarReader, header, destDir); err != nil {
			return err
		}
	}

	return nil
}

// untarEntry extracts the entry header describes, whose content tarReader is
// positioned at, refusing a name that escapes destDir. A name for destDir
// itself ("./", the first entry of most archives) is inside it.
func untarEntry(tarReader *tar.Reader, header *tar.Header, destDir string) error {
	destDir = filepath.Clean(destDir)
	target := filepath.Join(destDir, filepath.Clean(header.Name))

	if target != destDir && !strings.HasPrefix(target, destDir+string(os.PathSeparator)) {
		return fmt.Errorf("%w: %s", ErrPathTraversal, header.Name)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(target, dirPermissions); err != nil {
			return fmt.Errorf("mkdir %s: %w", header.Name, err)
		}
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), dirPermissions); err != nil {
			return fmt.Errorf("mkdir parent %s: %w", header.Name, err)
		}

		if err := extractFile(target, tarReader); err != nil {
			return fmt.Errorf("write %s: %w", header.Name, err)
		}
	default:
		// Skip unsupported entry types (symlinks, etc.).
	}

	return nil
}

func extractFile(path string, reader io.Reader) error {
	out, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePermissions) // #nosec G304 -- under destDir
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}

	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()

		return fmt.Errorf("copy: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

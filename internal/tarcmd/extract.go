package tarcmd

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/forkcloser/compress/internal/constants"
)

const (
	// specialBits are what -p restores beyond the permissions; os.Root
	// refuses them at creation, so they are applied afterwards.
	specialBits = fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky
)

var (
	errDotDot          = errors.New("Member name contains '..'")
	errIsDir           = errors.New("Is a directory")
	errUnsupportedType = errors.New("Cannot mknod: entry type is not supported")
	errLinkTarget      = errors.New("Cannot hard link: target not extracted")
)

// extractor writes an archive's entries under one directory, through an
// os.Root so that no entry, however named, lands or links outside it.
type extractor struct {
	opts   *options
	root   *os.Root
	stdout io.Writer
	stderr io.Writer

	// dirs are the directories extracted, in order, for the modes and
	// times set once their content is in place.
	dirs []dirEntry

	warnedLeadingSlash bool
	failed             bool
}

type dirEntry struct {
	name    string
	mode    os.FileMode
	modTime time.Time
}

// perm is the nine permission bits of a header's mode, what an entry is
// created with.
func perm(header *tar.Header) os.FileMode {
	return os.FileMode(header.Mode) & os.ModePerm // #nosec G115 -- masked to nine bits
}

// special is the mode -p restores: the permissions with the setuid, setgid
// and sticky bits.
func special(header *tar.Header) os.FileMode {
	return header.FileInfo().Mode() & (os.ModePerm | specialBits)
}

// extract reads every entry and extracts it; an entry's own failure is
// reported and the run goes on, a broken archive ends it.
func (e *extractor) extract(reader *tar.Reader) error {
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("Unexpected EOF in archive: %w", err)
		}

		e.entry(reader, header)
	}

	e.finishDirs()

	return nil
}

// entry extracts one header's entry, whose content reader is positioned at.
func (e *extractor) entry(reader *tar.Reader, header *tar.Header) {
	if header.Typeflag == tar.TypeXGlobalHeader {
		return
	}

	name, ok := e.memberName(header.Name)
	if !ok {
		return
	}

	if e.opts.verbose {
		fmt.Fprintln(e.stdout, header.Name)
	}

	if name == "" {
		return // the archive's own root, destDir itself
	}

	var err error

	switch header.Typeflag {
	case tar.TypeDir:
		err = e.dir(name, header)
	case tar.TypeReg:
		err = e.file(reader, name, header)
	case tar.TypeSymlink:
		err = e.symlink(name, header)
	case tar.TypeLink:
		err = e.hardlink(name, header)
	default:
		err = fmt.Errorf("%w: %q", errUnsupportedType, header.Typeflag)
	}

	if err != nil {
		e.reportf("%s: %v", header.Name, err)
	}
}

// memberName turns an archive name into the path under the root: forward
// slashes, a leading slash dropped (with a warning, once), the first
// --strip-components elements dropped (an entry with no more is skipped),
// and no ".." element (such an entry is refused). The second result is
// false for an entry to skip.
func (e *extractor) memberName(archiveName string) (string, bool) {
	name := archiveName

	if strings.HasPrefix(name, "/") {
		if !e.warnedLeadingSlash {
			e.warnedLeadingSlash = true

			fmt.Fprintln(e.stderr, "tar: Removing leading `/' from member names")
		}

		name = strings.TrimLeft(name, "/")
	}

	var elements []string

	for element := range strings.SplitSeq(name, "/") {
		switch element {
		case "", ".":
			continue
		case "..":
			e.reportf("%s: %v", archiveName, errDotDot)

			return "", false
		}

		elements = append(elements, element)
	}

	if len(elements) < e.opts.stripComponents {
		return "", false
	}

	return path.Join(elements[e.opts.stripComponents:]...), true
}

func (e *extractor) dir(name string, header *tar.Header) error {
	if err := e.root.MkdirAll(name, perm(header)|constants.OwnerAccess); err != nil {
		return fmt.Errorf("Cannot mkdir: %w", errReason(err))
	}

	e.dirs = append(e.dirs, dirEntry{name: name, mode: special(header), modTime: header.ModTime})

	return nil
}

func (e *extractor) file(reader io.Reader, name string, header *tar.Header) error {
	if err := e.prepare(name); err != nil {
		return err
	}

	// O_EXCL: prepare removed what was there, so a name that now exists is
	// one that appeared since, not one to follow.
	f, err := e.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm(header))
	if err != nil {
		return fmt.Errorf("Cannot open: %w", errReason(err))
	}

	if _, err := io.Copy(f, reader); err != nil {
		_ = f.Close()

		return fmt.Errorf("Cannot write: %w", errReason(err))
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("Cannot close: %w", errReason(err))
	}

	if e.opts.samePermissions {
		if err := e.root.Chmod(name, special(header)); err != nil {
			return fmt.Errorf("Cannot change mode: %w", errReason(err))
		}
	}

	if !e.opts.touch {
		if err := e.root.Chtimes(name, header.ModTime, header.ModTime); err != nil {
			return fmt.Errorf("Cannot utime: %w", errReason(err))
		}
	}

	return nil
}

func (e *extractor) symlink(name string, header *tar.Header) error {
	if err := e.prepare(name); err != nil {
		return err
	}

	if err := e.root.Symlink(header.Linkname, name); err != nil {
		return fmt.Errorf("Cannot create symlink to %q: %w", header.Linkname, errReason(err))
	}

	return nil
}

// hardlink links name to an entry extracted earlier; the link's target is an
// archive name too, subject to the same stripping.
func (e *extractor) hardlink(name string, header *tar.Header) error {
	target, ok := e.memberName(header.Linkname)
	if !ok || target == "" {
		return fmt.Errorf("%w: %q", errLinkTarget, header.Linkname)
	}

	if err := e.prepare(name); err != nil {
		return err
	}

	if err := e.root.Link(target, name); err != nil {
		return fmt.Errorf("Cannot hard link to %q: %w", header.Linkname, errReason(err))
	}

	return nil
}

// prepare makes room for a non-directory entry: its parent exists and
// nothing but a directory stays at its name (a directory there is an
// error, as it is for tar).
func (e *extractor) prepare(name string) error {
	if parent := path.Dir(name); parent != "." {
		if err := e.root.MkdirAll(parent, constants.DirPermissions); err != nil {
			return fmt.Errorf("Cannot mkdir: %w", errReason(err))
		}
	}

	info, err := e.root.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("Cannot stat: %w", errReason(err))
	}

	if info.IsDir() {
		return fmt.Errorf("Cannot open: %w", errIsDir)
	}

	if err := e.root.Remove(name); err != nil {
		return fmt.Errorf("Cannot unlink: %w", errReason(err))
	}

	return nil
}

// finishDirs applies the directories' modes (under -p) and times (unless
// -m), deepest first, so a parent's time is not moved by its children.
func (e *extractor) finishDirs() {
	for _, dir := range slices.Backward(e.dirs) {
		if e.opts.samePermissions {
			if err := e.root.Chmod(dir.name, dir.mode); err != nil {
				e.reportf("%s: Cannot change mode: %v", dir.name, errReason(err))
			}
		}

		if !e.opts.touch {
			if err := e.root.Chtimes(dir.name, dir.modTime, dir.modTime); err != nil {
				e.reportf("%s: Cannot utime: %v", dir.name, errReason(err))
			}
		}
	}
}

func (e *extractor) reportf(format string, args ...any) {
	e.failed = true

	fmt.Fprintf(e.stderr, "tar: "+format+"\n", args...)
}

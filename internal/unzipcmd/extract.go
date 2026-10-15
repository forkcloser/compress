package unzipcmd

import (
	"archive/zip"
	"encoding/binary"
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
	// linkTargetCap bounds a symbolic link's target read from the archive.
	linkTargetCap = 64 * 1024

	// The extra-field ids that carry a time: extended timestamp, NTFS, and
	// Info-ZIP's old unix field. An entry without one has only the DOS wall
	// clock, which Info-ZIP reads as local time.
	extTimeExtraID = 0x5455
	ntfsExtraID    = 0x000a
	unixExtraID    = 0x5855
)

// extractor writes an archive's entries under one directory, through an
// os.Root so that no entry, however named, lands or links outside it.
type extractor struct {
	opts   *options
	root   *os.Root
	stdout io.Writer
	stderr io.Writer
	status int

	// dirs are the directories extracted, in order, for the modes and
	// times set once their content is in place.
	dirs []dirEntry
}

type dirEntry struct {
	name    string
	mode    os.FileMode
	modTime time.Time
}

func (e *extractor) warnf(format string, args ...any) {
	if e.status == ExitOK {
		e.status = ExitWarning
	}

	fmt.Fprintf(e.stderr, format+"\n", args...)
}

func (e *extractor) errorf(format string, args ...any) {
	e.status = ExitError

	fmt.Fprintf(e.stderr, format+"\n", args...)
}

// entry extracts one entry: a directory, a symbolic link, or a file.
func (e *extractor) entry(file *zip.File) {
	name, ok := e.memberName(file.Name)
	if !ok {
		return
	}

	mode := file.Mode()

	switch {
	case strings.HasSuffix(file.Name, "/") || mode.IsDir():
		if e.opts.junk {
			return // -j makes no directories
		}

		e.directory(name, file, mode)
	case mode&fs.ModeSymlink != 0:
		e.symlink(name, file)
	default:
		e.file(name, file, mode)
	}
}

// memberName is where an entry lands: leading slashes and ".." components
// dropped with Info-ZIP's warning, the base name alone under -j; an empty
// result is nothing to extract.
func (e *extractor) memberName(raw string) (string, bool) {
	name := strings.TrimLeft(raw, "/")

	if e.opts.junk {
		name = path.Base(strings.TrimSuffix(name, "/"))
	}

	parts := strings.Split(name, "/")
	kept := parts[:0]
	skipped := false

	for _, part := range parts {
		if part == ".." {
			skipped = true

			continue
		}

		kept = append(kept, part)
	}

	if skipped {
		e.warnf("warning:  skipped \"../\" path component(s) in %s", raw)
	}

	name = strings.Trim(strings.Join(kept, "/"), "/")
	if name == "" || name == "." {
		return "", false
	}

	return name, true
}

// display is the name as Info-ZIP prints it: under the -d directory.
func (e *extractor) display(name string) string {
	if e.opts.exdir == "" {
		return name
	}

	return strings.TrimSuffix(e.opts.exdir, "/") + "/" + name
}

func (e *extractor) directory(name string, file *zip.File, mode os.FileMode) {
	perm := mode.Perm()
	if perm == 0 {
		perm = constants.DirPermissions
	}

	if err := e.root.MkdirAll(name, perm|constants.OwnerAccess); err != nil {
		e.errorf("checkdir error:  cannot create %s: %v", e.display(name), reason(err))

		return
	}

	e.dirs = append(e.dirs, dirEntry{name: name, mode: perm, modTime: modTime(file)})

	if !e.opts.quiet {
		fmt.Fprintf(e.stdout, "%11s: %s\n", "creating", e.display(name))
	}
}

// parents creates an entry's parent directories, as Info-ZIP does for an
// archive without directory entries.
func (e *extractor) parents(name string) bool {
	parent := path.Dir(name)
	if parent == "." {
		return true
	}

	if err := e.root.MkdirAll(parent, constants.DirPermissions); err != nil {
		e.errorf("checkdir error:  cannot create %s: %v", e.display(parent), reason(err))

		return false
	}

	return true
}

// exists is whether an entry's place is taken and must stay so: under -n
// silently, otherwise with the warning that stands for Info-ZIP's prompt.
// Under -o whatever is there is removed first.
func (e *extractor) exists(name string) bool {
	if e.opts.overwrite {
		_ = e.root.Remove(name)

		return false
	}

	if _, err := e.root.Lstat(name); err != nil {
		return false
	}

	if !e.opts.never {
		e.warnf("replace %s? not replaced: use -o to overwrite or -n to keep", e.display(name))
	}

	return true
}

func (e *extractor) file(name string, file *zip.File, mode os.FileMode) {
	if !e.parents(name) || e.exists(name) {
		return
	}

	perm := mode.Perm()
	if perm == 0 {
		perm = constants.FilePermissions
	}

	out, err := e.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		e.errorf("%s:  cannot create: %v", e.display(name), reason(err))

		return
	}

	if err := copyContent(out, file); err != nil {
		_ = out.Close()
		_ = e.root.Remove(name)

		e.errorf("%s:  %v", e.display(name), err)

		return
	}

	if err := out.Close(); err != nil {
		e.errorf("%s:  %v", e.display(name), reason(err))

		return
	}

	// The stored mode exactly, as Info-ZIP restores it: the create above went
	// through the umask.
	if err := e.root.Chmod(name, perm); err != nil {
		e.warnf("%s:  cannot set mode: %v", e.display(name), reason(err))
	}

	// Info-ZIP restores the time it stored; a refusal is not worth the entry.
	_ = e.root.Chtimes(name, modTime(file), modTime(file))

	if !e.opts.quiet {
		verb := "inflating"
		if file.Method == zip.Store {
			verb = "extracting"
		}

		fmt.Fprintf(e.stdout, "%11s: %-24s\n", verb, e.display(name))
	}
}

// copyContent writes an entry's data; the archive's CRC is checked at its end.
func copyContent(out io.Writer, file *zip.File) error {
	content, err := file.Open()
	if err != nil {
		return fmt.Errorf("cannot open entry: %w", err)
	}

	defer func() { _ = content.Close() }() // read-only: a close error carries nothing

	if _, err := io.Copy(out, content); err != nil { // #nosec G110 -- extracting the entry is the command
		if errors.Is(err, zip.ErrChecksum) {
			return errors.New("bad CRC") //nolint:err113 // Info-ZIP's words
		}

		return fmt.Errorf("invalid compressed data to inflate: %w", err)
	}

	return nil
}

func (e *extractor) symlink(name string, file *zip.File) {
	if !e.parents(name) || e.exists(name) {
		return
	}

	content, err := file.Open()
	if err != nil {
		e.errorf("%s:  cannot open entry: %v", e.display(name), err)

		return
	}

	target, err := io.ReadAll(io.LimitReader(content, linkTargetCap))
	_ = content.Close()

	if err != nil {
		e.errorf("%s:  %v", e.display(name), err)

		return
	}

	if err := e.root.Symlink(string(target), name); err != nil {
		e.errorf("%s:  cannot create symbolic link: %v", e.display(name), reason(err))

		return
	}

	if !e.opts.quiet {
		fmt.Fprintf(e.stdout, "%11s: %s  -> %s\n", "linking", e.display(name), target)
	}
}

// finishDirs applies the directories' modes and times, deepest first, now
// that nothing more lands under them.
func (e *extractor) finishDirs() {
	for _, dir := range slices.Backward(e.dirs) {
		if err := e.root.Chmod(dir.name, dir.mode); err != nil {
			e.warnf("%s:  cannot set mode: %v", e.display(dir.name), reason(err))
		}

		_ = e.root.Chtimes(dir.name, dir.modTime, dir.modTime)
	}
}

// modTime is an entry's time as Info-ZIP restores it: the extended
// timestamp when the archive carries one, otherwise the DOS wall clock read
// as local time.
func modTime(file *zip.File) time.Time {
	if hasExtendedTime(file.Extra) {
		return file.Modified
	}

	wall := file.Modified
	local := time.Local //nolint:gosmopolitan // the DOS fields are local time, as Info-ZIP reads them

	return time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), 0, local)
}

// hasExtendedTime is whether an entry's extra fields carry a time.
func hasExtendedTime(extra []byte) bool {
	const headerLen = 4

	for len(extra) >= headerLen {
		tag := binary.LittleEndian.Uint16(extra)
		size := int(binary.LittleEndian.Uint16(extra[2:]))

		if tag == extTimeExtraID || tag == ntfsExtraID || tag == unixExtraID {
			return true
		}

		extra = extra[min(headerLen+size, len(extra)):]
	}

	return false
}

// reason is the OS's reason alone: the path is already in the message.
func reason(err error) error {
	if pathErr, ok := errors.AsType[*os.PathError](err); ok {
		return pathErr.Err
	}

	if linkErr, ok := errors.AsType[*os.LinkError](err); ok {
		return linkErr.Err
	}

	return err
}

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

	"github.com/forkcloser/go-compress/internal/constants"
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

	// encryptedFlag is the general-purpose bit of an entry whose data is
	// encrypted.
	encryptedFlag = 0x1

	// The creators, in the high byte of an entry's creator version, whose
	// entries Info-ZIP takes a unix mode from; every other leaves DOS
	// attributes, of which the read-only bit is the one that maps to a mode.
	creatorFAT  = 0
	creatorUnix = 3
	dosReadOnly = 0x1

	// What an entry without a unix mode is created with, for the umask to
	// take from.
	anyFile      os.FileMode = 0o666
	anyDirectory os.FileMode = 0o777
	writeBits    os.FileMode = 0o222
	ownerBits    os.FileMode = 0o700
)

// The entries Info-ZIP skips rather than fails, in its words.
var (
	errPassword = errors.New("unable to get password")
	errMethod   = errors.New("unsupported compression method")
)

// extractor writes an archive's entries under one directory, through an
// os.Root so that no entry, however named, lands or links outside it.
type extractor struct {
	opts   *options
	root   *os.Root
	stdout io.Writer
	stderr io.Writer
	status int

	// dirs are the directories an entry created, in order, for the modes
	// and times set once their content is in place.
	dirs []dirEntry
}

type dirEntry struct {
	name    string
	mode    os.FileMode
	modTime time.Time
}

// permissions is the mode an entry is created with, and whether it is one
// the archive stored, to be set exactly once the entry is written. An entry
// from a creator without unix modes has none: archive/zip makes up 0666 for
// it, which set exactly would go past the umask and leave a directory
// nobody can enter. Its mode is what the umask leaves at creation, less the
// write bits when the archive has it read-only.
func permissions(file *zip.File, mode, fallback, anything os.FileMode) (os.FileMode, bool) {
	if file.ExternalAttrs&dosReadOnly != 0 {
		anything &^= writeBits
	}

	switch file.CreatorVersion >> 8 {
	case creatorUnix:
		if perm := mode.Perm(); perm != 0 {
			return perm, true
		}

		return fallback, true
	case creatorFAT:
		// A unix tool that signs as FAT keeps its mode where unix does;
		// Info-ZIP takes it when the owner's bits agree with the DOS ones.
		if perm := os.FileMode(file.ExternalAttrs >> 16).Perm(); perm&ownerBits == anything&ownerBits {
			return perm, true
		}
	}

	return anything, false
}

func (e *extractor) warnf(format string, args ...any) {
	if e.status == ExitOK {
		e.status = ExitWarning
	}

	fmt.Fprintf(e.stderr, format+"\n", args...)
}

func (e *extractor) errorf(format string, args ...any) {
	e.status = worse(e.status, ExitError)

	fmt.Fprintf(e.stderr, format+"\n", args...)
}

// open is an entry's data, or nothing once the reason is reported. It comes
// before anything is done in the entry's place: what cannot be read
// replaces nothing.
func (e *extractor) open(name string, file *zip.File) (io.ReadCloser, bool) {
	data, err := content(file)
	if err == nil {
		return data, true
	}

	if status := skipped(e.stderr, file.Name, err, e.opts.quiet); status != ExitOK {
		e.status = worse(e.status, status)
	} else {
		e.errorf("%s:  %v", e.display(name), err)
	}

	return nil, false
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
	if !e.parents(name) {
		return
	}

	perm, stored := permissions(file, mode, constants.DirPermissions, anyDirectory)

	// A directory that is already there is not this run's to give a mode or
	// a time to: the entry creates nothing, and nothing is said of it.
	if err := e.root.Mkdir(name, perm|constants.OwnerAccess); err != nil {
		if info, statErr := e.root.Stat(name); statErr != nil || !info.IsDir() {
			e.errorf("checkdir error:  cannot create %s: %v", e.display(name), reason(err))
		}

		return
	}

	if !stored {
		// A mode made up here takes nothing back from the umask the
		// directory was created through.
		info, err := e.root.Stat(name)
		if err != nil {
			e.warnf("%s:  cannot set mode: %v", e.display(name), reason(err))

			return
		}

		perm &= info.Mode().Perm()
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
	data, ok := e.open(name, file)
	if !ok {
		return
	}

	defer func() { _ = data.Close() }() // read-only: a close error carries nothing

	if !e.parents(name) || e.exists(name) {
		return
	}

	perm, stored := permissions(file, mode, constants.FilePermissions, anyFile)

	out, err := e.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		e.errorf("%s:  cannot create: %v", e.display(name), reason(err))

		return
	}

	if err := copyData(out, data); err != nil {
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
	// through the umask, which is where a mode made up here has to stay.
	if stored {
		if err := e.root.Chmod(name, perm); err != nil {
			e.warnf("%s:  cannot set mode: %v", e.display(name), reason(err))
		}
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

// content opens an entry's data. An encrypted entry would open and inflate
// as garbage, so it is refused here, as is a method with no decompressor.
func content(file *zip.File) (io.ReadCloser, error) {
	if file.Flags&encryptedFlag != 0 {
		return nil, errPassword
	}

	data, err := file.Open()
	if errors.Is(err, zip.ErrAlgorithm) {
		return nil, fmt.Errorf("%w %d", errMethod, file.Method)
	}

	if err != nil {
		return nil, fmt.Errorf("cannot open entry: %w", err)
	}

	return data, nil
}

// skipped writes Info-ZIP's line for an entry it skips, under the name the
// archive has for it, and is the status that leaves the run with: none for
// any other error. Quiet drops the line for a method, never the password's.
func skipped(out io.Writer, name string, err error, quiet bool) int {
	switch {
	case errors.Is(err, errPassword):
		fmt.Fprintf(out, "%11s: %-22s  %v\n", "skipping", name, err)

		return ExitPassword
	case errors.Is(err, errMethod):
		if !quiet {
			fmt.Fprintf(out, "%11s: %-22s  %v\n", "skipping", name, err)
		}

		return ExitUnsupported
	default:
		return ExitOK
	}
}

// copyContent writes an entry's data; the archive's CRC is checked at its end.
func copyContent(out io.Writer, file *zip.File) error {
	data, err := content(file)
	if err != nil {
		return err
	}

	defer func() { _ = data.Close() }() // read-only: a close error carries nothing

	return copyData(out, data)
}

func copyData(out io.Writer, data io.Reader) error {
	if _, err := io.Copy(out, data); err != nil { // #nosec G110 -- extracting the entry is the command
		if errors.Is(err, zip.ErrChecksum) {
			return errors.New("bad CRC") //nolint:err113 // Info-ZIP's words
		}

		return fmt.Errorf("invalid compressed data to inflate: %w", err)
	}

	return nil
}

func (e *extractor) symlink(name string, file *zip.File) {
	data, ok := e.open(name, file)
	if !ok {
		return
	}

	target, err := io.ReadAll(io.LimitReader(data, linkTargetCap))
	_ = data.Close()

	if err != nil {
		e.errorf("%s:  %v", e.display(name), err)

		return
	}

	if !e.parents(name) || e.exists(name) {
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

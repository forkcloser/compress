package tarcmd

import (
	"archive/tar"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/forkcloser/xos"
)

// creator writes members into one archive. A member that cannot be archived
// is reported and the run goes on; the archive stays well-formed either way.
type creator struct {
	opts    *options
	writer  *tar.Writer
	verbose io.Writer // where -v prints names: stdout, or stderr when the archive is stdout
	stderr  io.Writer
	archive os.FileInfo // the file the archive is written to, nil when it is not a regular file

	// given is the command-line member being archived, as it was named,
	// and base the archive name it got: --exclude matches a name as the
	// member gave it, before its leading slash was removed.
	given, base string

	warnedLeadingSlash bool
	failed             bool
	changed            bool // a member changed while it was read: the archive is whole, and not the tree
}

// addMember archives one command-line member, or every name in its -T list.
func (c *creator) addMember(entry member) error {
	if entry.list == "" {
		return c.add(entry.dir, entry.name)
	}

	names, err := readList(entry.list)
	if err != nil {
		return err
	}

	for _, name := range names {
		if err := c.add(entry.dir, name); err != nil {
			return err
		}
	}

	return nil
}

// readList reads a -T file: one member name per line.
func readList(file string) ([]string, error) {
	f, err := xos.Open(file)
	if err != nil {
		return nil, fmt.Errorf("%s: Cannot open: %w", file, errReason(err))
	}

	defer func() { _ = f.Close() }() // read-only: a close error carries nothing

	var names []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			names = append(names, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: Cannot read: %w", file, errReason(err))
	}

	return names, nil
}

// add archives the member named name relative to dir, and what is under it
// unless --no-recursion. The archive name is the name as given, slashes
// forward, a leading slash removed with a warning the first time.
func (c *creator) add(dir, name string) error {
	archiveName := c.archiveName(name)

	c.given, c.base = strings.TrimRight(filepath.ToSlash(name), "/"), archiveName
	if c.excludes(archiveName) {
		return nil
	}

	// An absolute member is where it says, whatever -C is in force.
	fsPath := name
	if dir != "." && !filepath.IsAbs(name) {
		fsPath = filepath.Join(dir, name)
	}

	info, err := os.Lstat(fsPath)
	if err != nil {
		c.reportf("%s: Cannot stat: %v", name, errReason(err))

		return nil
	}

	return c.addPath(fsPath, archiveName, info)
}

// excludes reports whether --exclude leaves out the entry that gets
// archiveName, the member being archived or a name under it.
func (c *creator) excludes(archiveName string) bool {
	return excluded(c.opts.excludes, c.given+archiveName[len(c.base):])
}

// archiveName turns a command-line name into the member name: forward
// slashes, no leading slash, no trailing slash (the directory entry adds its
// own), "." kept as given so a tree archived as "." is "./…". Under -P the
// name is kept as given, its leading slash and ".." included.
func (c *creator) archiveName(name string) string {
	archiveName := filepath.ToSlash(name)

	if c.opts.absoluteNames {
		if trimmed := strings.TrimSuffix(archiveName, "/"); trimmed != "" {
			return trimmed
		}

		return archiveName
	}

	if strings.HasPrefix(archiveName, "/") {
		if !c.warnedLeadingSlash {
			c.warnedLeadingSlash = true

			fmt.Fprintln(c.stderr, "tar: Removing leading `/' from member names")
		}

		archiveName = strings.TrimLeft(archiveName, "/")
	}

	if archiveName == "" {
		return "."
	}

	return strings.TrimSuffix(archiveName, "/")
}

// addPath archives one path with the name it gets, then its children for a
// directory. The entry's own error ends the entry, not the run.
func (c *creator) addPath(fsPath, archiveName string, info os.FileInfo) error {
	header, err := c.header(fsPath, archiveName, info)
	if err != nil {
		c.reportf("%s: %v", archiveName, err)

		return nil
	}

	if header == nil {
		return nil
	}

	// The file is opened before its header is written: one that cannot be
	// read leaves no header to fill, and the archive stays whole.
	var content *os.File

	if header.Typeflag == tar.TypeReg {
		content, err = xos.Open(fsPath)
		if err != nil {
			c.reportf("%s: Cannot open: %v", archiveName, errReason(err))

			return nil
		}

		defer func() { _ = content.Close() }() // read-only: a close error carries nothing
	}

	if c.opts.verbose {
		fmt.Fprintln(c.verbose, header.Name)
	}

	if err := c.writer.WriteHeader(header); err != nil {
		if errors.Is(err, tar.ErrFieldTooLong) {
			c.reportf("%s: file name is too long (max 100); not dumped", archiveName)

			return nil
		}

		return fmt.Errorf("%s: %w", archiveName, err)
	}

	if content != nil {
		if err := c.copyContent(content, header, info); err != nil {
			return fmt.Errorf("%s: %w", archiveName, err)
		}
	}

	if !info.IsDir() || c.opts.noRecursion {
		return nil
	}

	return c.addChildren(fsPath, archiveName)
}

// addChildren archives a directory's entries, in the directory's order or
// sorted by name under --sort=name.
func (c *creator) addChildren(fsPath, archiveName string) error {
	dir, err := xos.Open(fsPath)
	if err != nil {
		c.reportf("%s: Cannot open: %v", archiveName, errReason(err))

		return nil
	}

	entries, err := dir.ReadDir(-1)

	_ = dir.Close() // read-only: a close error carries nothing

	if err != nil {
		c.reportf("%s: Cannot read directory: %v", archiveName, errReason(err))

		return nil
	}

	if c.opts.sortNames {
		slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	}

	for _, entry := range entries {
		childPath := filepath.Join(fsPath, entry.Name())
		// Not path.Join: it would clean "./sub" to "sub", and the names are
		// kept as the member was given.
		childName := archiveName + "/" + entry.Name()
		if c.excludes(childName) {
			continue
		}

		info, err := os.Lstat(childPath)
		if err != nil {
			c.reportf("%s: Cannot stat: %v", childName, errReason(err))

			continue
		}

		if err := c.addPath(childPath, childName, info); err != nil {
			return err
		}
	}

	c.checkUnchanged(fsPath, archiveName, entries)

	return nil
}

// checkUnchanged reads the directory again once its entries are archived
// and reports it when the names are not the ones first read. The archive
// then holds the first reading, short of the tree or past it, and nothing
// else says so: no entry failed. The run goes on, and its status says a
// member changed.
// Excluded names are no part of the archive and are left out of both
// readings: what a caller excludes is often what moves.
func (c *creator) checkUnchanged(fsPath, archiveName string, entries []os.DirEntry) {
	dir, err := xos.Open(fsPath)
	if err != nil {
		c.changedf("%s: file changed as we read it", archiveName)

		return
	}

	again, err := dir.ReadDir(-1)

	_ = dir.Close() // read-only: a close error carries nothing

	if err != nil || !slices.Equal(c.archived(archiveName, entries), c.archived(archiveName, again)) {
		c.changedf("%s: file changed as we read it", archiveName)
	}
}

// archived is the names of a directory's entries that are not excluded,
// sorted.
func (c *creator) archived(archiveName string, entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if !c.excludes(archiveName + "/" + entry.Name()) {
			names = append(names, entry.Name())
		}
	}

	slices.Sort(names)

	return names
}

// header builds the entry for one path: the file's own metadata, then the
// options' overrides. A socket has no entry and is nil, and so is the
// archive itself, met in the tree it is written in.
func (c *creator) header(fsPath, archiveName string, info os.FileInfo) (*tar.Header, error) {
	if info.Mode()&os.ModeSocket != 0 {
		fmt.Fprintf(c.stderr, "tar: %s: socket ignored\n", archiveName)

		return nil, nil //nolint:nilnil // no entry, and nothing wrong
	}

	// By identity, not by name: a hard link to the archive is the archive.
	if c.archive != nil && os.SameFile(c.archive, info) {
		fmt.Fprintf(c.stderr, "tar: %s: archive cannot contain itself; not dumped\n", archiveName)

		return nil, nil //nolint:nilnil // no entry, and nothing wrong
	}

	link := ""

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(fsPath)
		if err != nil {
			return nil, fmt.Errorf("Cannot readlink: %w", errReason(err))
		}

		link = target
	}

	header, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return nil, err //nolint:wrapcheck // the message names the entry already
	}

	header.Name = archiveName
	if info.IsDir() {
		header.Name += "/"
	}

	// Access and change times are neither in ustar nor reproducible; a
	// directory's or file's mtime is what the archive carries.
	header.AccessTime, header.ChangeTime = time.Time{}, time.Time{}
	header.Format = c.opts.format

	if c.opts.owner != nil {
		header.Uid, header.Uname = c.opts.owner.id, c.opts.owner.name
	}

	if c.opts.group != nil {
		header.Gid, header.Gname = c.opts.group.id, c.opts.group.name
	}

	if c.opts.numericOwner {
		header.Uname, header.Gname = "", ""
	}

	if c.opts.mtime != nil {
		header.ModTime = *c.opts.mtime
	}

	return header, nil
}

// copyContent writes exactly the size the header announced, whatever
// becomes of the file meanwhile, so the archive stays well-formed: one that
// grows is cut at that size, one that shrinks is filled with zeros to it.
// Either is said, as is a file whose size or time is no longer what the
// header was built from once it is read.
func (c *creator) copyContent(f *os.File, header *tar.Header, info os.FileInfo) error {
	copied, err := io.CopyN(c.writer, f, header.Size)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("Cannot write: %w", errReason(err))
	}

	if missing := header.Size - copied; missing > 0 {
		if _, err := io.CopyN(c.writer, zeros{}, missing); err != nil {
			return fmt.Errorf("Cannot write: %w", errReason(err))
		}

		c.changedf("%s: File shrank by %d bytes; padding with zeros", header.Name, missing)

		return nil
	}

	if now, err := f.Stat(); err != nil || now.Size() != info.Size() || !now.ModTime().Equal(info.ModTime()) {
		c.changedf("%s: file changed as we read it", header.Name)
	}

	return nil
}

// zeros reads as zeros without end.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
}

// changedf reports a member that is not, in the archive, what it is in the
// tree any more: the status says so, and it is not a failure.
func (c *creator) changedf(format string, args ...any) {
	c.changed = true

	fmt.Fprintf(c.stderr, "tar: "+format+"\n", args...)
}

func (c *creator) reportf(format string, args ...any) {
	c.failed = true

	fmt.Fprintf(c.stderr, "tar: "+format+"\n", args...)
}

// errReason is the OS's reason alone: the path is already in the message.
func errReason(err error) error {
	if pathErr, ok := errors.AsType[*os.PathError](err); ok {
		return pathErr.Err
	}

	if linkErr, ok := errors.AsType[*os.LinkError](err); ok {
		return linkErr.Err
	}

	return err
}

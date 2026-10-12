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
)

var errShortRead = errors.New("file shrank while being read")

// creator writes members into one archive. A member that cannot be archived
// is reported and the run goes on; the archive stays well-formed either way.
type creator struct {
	opts    *options
	writer  *tar.Writer
	verbose io.Writer // where -v prints names: stdout, or stderr when the archive is stdout
	stderr  io.Writer

	warnedLeadingSlash bool
	failed             bool
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
	f, err := os.Open(file) // #nosec G304 -- the list is the command's own -T argument
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

// archiveName turns a command-line name into the member name: forward
// slashes, no leading slash, no trailing slash (the directory entry adds its
// own), "." kept as given so a tree archived as "." is "./…".
func (c *creator) archiveName(name string) string {
	archiveName := filepath.ToSlash(name)

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
		content, err = os.Open(fsPath) // #nosec G304 -- the file is a member the command was given, or under one
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
		if err := c.copyContent(content, header); err != nil {
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
	dir, err := os.Open(fsPath) // #nosec G304 -- the directory is a member the command was given, or under one
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

		info, err := os.Lstat(childPath)
		if err != nil {
			c.reportf("%s: Cannot stat: %v", childName, errReason(err))

			continue
		}

		if err := c.addPath(childPath, childName, info); err != nil {
			return err
		}
	}

	return nil
}

// header builds the entry for one path: the file's own metadata, then the
// options' overrides. A socket has no entry and is nil.
func (c *creator) header(fsPath, archiveName string, info os.FileInfo) (*tar.Header, error) {
	if info.Mode()&os.ModeSocket != 0 {
		fmt.Fprintf(c.stderr, "tar: %s: socket ignored\n", archiveName)

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

// copyContent writes exactly the size the header announced, so a file that
// grows while read leaves the archive well-formed, and one that shrinks is
// an error rather than a truncated archive.
func (c *creator) copyContent(f *os.File, header *tar.Header) error {
	n, err := io.CopyN(c.writer, f, header.Size)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: %d of %d bytes", errShortRead, n, header.Size)
		}

		return fmt.Errorf("Cannot write: %w", errReason(err))
	}

	return nil
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

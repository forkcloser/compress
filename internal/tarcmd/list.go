package tarcmd

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// ugsWidth is the width GNU tar starts the "user/group size" columns at and
// widens as entries demand, so a listing's columns stay aligned.
const ugsWidth = 18

// lister prints an archive's entries, names only or the long form of -tv.
type lister struct {
	opts    *options
	members *selection
	stdout  io.Writer
	stderr  io.Writer
	width   int
	failed  bool
}

func (l *lister) list(reader *tar.Reader) error {
	l.width = ugsWidth

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("Unexpected EOF in archive: %w", err)
		}

		if header.Typeflag == tar.TypeXGlobalHeader || !l.members.take(header.Name) {
			continue
		}

		if l.opts.verbose {
			l.long(header)
		} else {
			fmt.Fprintln(l.stdout, header.Name)
		}
	}

	for _, name := range l.members.missing() {
		l.failed = true

		fmt.Fprintf(l.stderr, "tar: %s: Not found in archive\n", name)
	}

	return nil
}

// long prints one entry as -tv does: mode, user/group, size, date, name,
// and the link target of a symlink or hard link.
func (l *lister) long(header *tar.Header) {
	owner := l.owner(header.Uname, header.Uid) + "/" + l.owner(header.Gname, header.Gid)

	size := strconv.FormatInt(header.Size, 10)
	if header.Typeflag == tar.TypeChar || header.Typeflag == tar.TypeBlock {
		size = strconv.FormatInt(header.Devmajor, 10) + "," + strconv.FormatInt(header.Devminor, 10)
	}

	if need := len(owner) + 1 + len(size); need > l.width {
		l.width = need
	}

	when := header.ModTime.Local() //nolint:gosmopolitan // local time, as GNU tar lists it

	fmt.Fprintf(l.stdout, "%s %s %*s %s %s", modeString(header), owner, l.width-len(owner)-1, size,
		when.Format("2006-01-02 15:04"), header.Name)

	switch header.Typeflag {
	case tar.TypeSymlink:
		fmt.Fprintf(l.stdout, " -> %s", header.Linkname)
	case tar.TypeLink:
		fmt.Fprintf(l.stdout, " link to %s", header.Linkname)
	}

	fmt.Fprintln(l.stdout)
}

func (l *lister) owner(name string, id int) string {
	if name == "" || l.opts.numericOwner {
		return strconv.Itoa(id)
	}

	return name
}

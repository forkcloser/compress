package zipcmd

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/flate"

	"github.com/forkcloser/compress/internal/constants"
)

const (
	// dosEpoch is the first year the DOS date field can hold.
	dosEpoch = 1980
	percent  = 100
)

var errRepeated = errors.New("cannot repeat names in zip file")

// entry is one input to store: its path on disk and its name in the archive.
type entry struct {
	path string
	name string
	info fs.FileInfo
}

// creator builds or updates one archive.
type creator struct {
	opts   *options
	stdout io.Writer
	stderr io.Writer

	// pending is the entry whose line waits for its sizes, which the writer
	// settles only when the next entry begins or the archive closes.
	pending *pendingEntry
	latest  time.Time
	status  int
}

type pendingEntry struct {
	verb   string
	header *zip.FileHeader
}

func (c *creator) warnf(format string, args ...any) {
	fmt.Fprintf(c.stderr, "\tzip warning: "+format+"\n", args...)
}

func (c *creator) errorf(status int, format string, args ...any) int {
	fmt.Fprintf(c.stderr, "\nzip error: "+format+"\n", args...)

	return status
}

func (c *creator) run() int {
	entries, err := c.collect()
	if err != nil {
		return c.errorf(ExitUsage, "%v (%v)", errUsage, err)
	}

	if c.opts.archive == "-" {
		if len(entries) == 0 {
			return c.errorf(ExitNothing, "Nothing to do! (-)")
		}

		return c.write(c.stdout, nil, entries)
	}

	target := archiveName(c.opts.archive)

	existing, err := openExisting(target)
	if err != nil {
		return c.errorf(ExitStructure, "Zip file structure invalid (%s)", target)
	}

	if existing != nil {
		defer func() { _ = existing.Close() }()
	}

	if len(entries) == 0 {
		if existing != nil && c.opts.latestTime {
			return c.touch(target, existing)
		}

		return c.errorf(ExitNothing, "Nothing to do! (%s)", target)
	}

	return c.writeFile(target, existing, entries)
}

// archiveName is Info-ZIP's: a name with no extension gets ".zip".
func archiveName(name string) string {
	if !strings.Contains(filepath.Base(name), ".") {
		return name + ".zip"
	}

	return name
}

// openExisting is the archive to update, nil when there is none yet.
func openExisting(target string) (*zip.ReadCloser, error) {
	existing, err := zip.OpenReader(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // no archive is a state, not a failure
	}

	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", target, err)
	}

	return existing, nil
}

// collect resolves the inputs to entries: a missing input is a warning, a
// directory is its own entry and, with -r, everything under it; a name
// stored twice is Info-ZIP's usage error.
func (c *creator) collect() ([]entry, error) {
	var entries []entry

	for _, input := range c.opts.inputs {
		info, err := os.Stat(input)
		if err != nil {
			c.warnf("name not matched: %s", input)

			continue
		}

		if info.IsDir() {
			entries = c.directory(entries, input)

			continue
		}

		entries = c.add(entries, input, info)
	}

	seen := make(map[string]string, len(entries))

	for _, item := range entries {
		if first, dup := seen[item.name]; dup {
			c.warnf("first full name: %s\n\tzip warning: second full name: %s\n\t"+
				"zip warning: name in zip file repeated: %s", first, item.path, item.name)

			return nil, errRepeated
		}

		seen[item.name] = item.path
	}

	return entries, nil
}

// directory adds a directory: its own entry unless -D or -j, and with -r
// its content, in name order. A symbolic link under it is stored as what
// it points to, as Info-ZIP does without -y; one that points to a
// directory is stored as that directory's entry alone, not walked.
func (c *creator) directory(entries []entry, dir string) []entry {
	if !c.opts.recurse {
		info, err := os.Stat(dir)
		if err != nil {
			c.warnf("name not matched: %s", dir)

			return entries
		}

		return c.add(entries, dir, info)
	}

	err := filepath.WalkDir(dir, func(fsPath string, _ fs.DirEntry, err error) error {
		if err != nil {
			c.warnf("could not open for reading: %s", fsPath)
			c.status = ExitInput

			return nil //nolint:nilerr // reported; the walk goes on, as Info-ZIP's does
		}

		// Stat, not the entry's Lstat: a link is stored as its target, the
		// mode and content both.
		info, err := os.Stat(fsPath)
		if err != nil {
			c.warnf("name not matched: %s", fsPath)

			return nil //nolint:nilerr // reported; the walk goes on, as Info-ZIP's does
		}

		entries = c.add(entries, fsPath, info)

		return nil
	})
	if err != nil {
		// WalkDir's own failures are all reported through the callback.
		c.warnf("could not open for reading: %s", dir)
		c.status = ExitInput
	}

	return entries
}

// add appends one input under its archive name, or skips it: a directory
// under -D or -j, a name that would escape the archive's root.
func (c *creator) add(entries []entry, fsPath string, info fs.FileInfo) []entry {
	if info.IsDir() && (c.opts.noDirEntries || c.opts.junkPaths) {
		return entries
	}

	name, ok := c.storedName(fsPath, info.IsDir())
	if !ok {
		return entries
	}

	return append(entries, entry{path: fsPath, name: name, info: info})
}

// storedName is the archive name for a path: forward slashes, no leading
// "./" or "/", a trailing "/" on a directory; the base name alone under -j.
// A name with a ".." component, or nothing left, is not stored.
func (c *creator) storedName(fsPath string, isDir bool) (string, bool) {
	name := path.Clean(filepath.ToSlash(fsPath))
	name = strings.TrimLeft(name, "/")

	if c.opts.junkPaths {
		name = path.Base(name)
	}

	if name == "." || name == ".." || strings.HasPrefix(name, "../") {
		if name != "." {
			c.warnf("name not matched: %s", fsPath)
		}

		return "", false
	}

	if isDir {
		name += "/"
	}

	return name, true
}

// writeFile writes the archive beside its final name and moves it into
// place, so a failure leaves whatever was there.
func (c *creator) writeFile(target string, existing *zip.ReadCloser, entries []entry) int {
	tmp, err := os.CreateTemp(filepath.Dir(target), "zi")
	if err != nil {
		return c.errorf(ExitCreate, "Could not create output file (%v)", err)
	}

	defer func() { _ = os.Remove(tmp.Name()) }()

	if status := c.write(tmp, existing, entries); status != ExitOK && status != ExitInput {
		_ = tmp.Close()

		return status
	}

	if err := tmp.Close(); err != nil {
		return c.errorf(ExitCreate, "Output file write failure (%v)", err)
	}

	if err := os.Chmod(tmp.Name(), constants.FilePermissions); err != nil {
		return c.errorf(ExitCreate, "Could not create output file (%v)", err)
	}

	if existing != nil {
		// The update replaces the archive being read: release it first, for
		// the platforms that refuse to replace an open file.
		_ = existing.Close()
	}

	if err := os.Rename(tmp.Name(), target); err != nil {
		return c.errorf(ExitCreate, "Could not create output file (%v)", err)
	}

	if c.opts.latestTime && !c.latest.IsZero() {
		_ = os.Chtimes(target, c.latest, c.latest)
	}

	return c.status
}

// write writes the archive: an existing archive's entries in their order,
// each replaced by the input of its name, then the inputs left.
func (c *creator) write(out io.Writer, existing *zip.ReadCloser, entries []entry) int {
	writer := zip.NewWriter(out)
	writer.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, c.opts.level)
	})

	replacing := make(map[string]int, len(entries))
	for i, e := range entries {
		replacing[e.name] = i
	}

	done := make([]bool, len(entries))

	if existing != nil {
		for _, file := range existing.File {
			if i, ok := replacing[file.Name]; ok {
				done[i] = true

				c.store(writer, entries[i], "updating")

				continue
			}

			c.noteTime(file.Modified)

			if err := writer.Copy(file); err != nil {
				return c.errorf(ExitCreate, "Output file write failure (%v)", err)
			}
		}
	}

	for i, e := range entries {
		if !done[i] {
			c.store(writer, e, "adding")
		}
	}

	if err := writer.Close(); err != nil {
		return c.errorf(ExitCreate, "Output file write failure (%v)", err)
	}

	c.flush()

	return c.status
}

// store writes one entry, opening a file before its header so that an input
// that cannot be read leaves no trace in the archive.
func (c *creator) store(writer *zip.Writer, item entry, verb string) {
	var source *os.File

	if !item.info.IsDir() {
		var err error

		source, err = os.Open(item.path) // #nosec G304 -- the file is the command's own argument
		if err != nil {
			c.warnf("could not open for reading: %s", item.path)
			c.status = ExitInput

			return
		}

		defer func() { _ = source.Close() }() // read-only: a close error carries nothing
	}

	header := c.header(item)

	content, err := writer.CreateHeader(header)
	if err != nil {
		c.warnf("could not write %s: %v", item.name, err)
		c.status = ExitCreate

		return
	}

	c.flush()
	c.pending = &pendingEntry{verb: verb, header: header}

	if source == nil {
		return
	}

	if _, err := io.Copy(content, source); err != nil {
		c.warnf("could not read %s: %v", item.path, err)
		c.status = ExitInput
	}
}

// header is an entry's: Info-ZIP's unix attributes, stored for a directory
// or under -0, deflated otherwise; the time as the extended timestamp, or
// as the DOS fields alone under -X, both in the local wall clock.
func (c *creator) header(item entry) *zip.FileHeader {
	header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
	header.SetMode(item.info.Mode())

	if item.info.IsDir() || c.opts.level == 0 {
		header.Method = zip.Store
	}

	modTime := item.info.ModTime()
	c.noteTime(modTime)

	if c.opts.stripExtra {
		// The DOS fields alone: Modified would add the extended-timestamp
		// extra field that -X is there to leave out.
		header.ModifiedDate, header.ModifiedTime = dosTime(modTime) //nolint:staticcheck // SA1019: deliberate
	} else {
		header.Modified = modTime
	}

	return header
}

// dosTime is a time as the DOS date and time fields hold it: the local wall
// clock, to two seconds, no earlier than 1980.
func dosTime(when time.Time) (date, clock uint16) {
	if when.Year() < dosEpoch {
		when = time.Date(dosEpoch, time.January, 1, 0, 0, 0, 0, when.Location())
	}

	date = uint16(when.Day() | int(when.Month())<<5 | (when.Year()-dosEpoch)<<9) // #nosec G115 -- 5, 4 and 7 bits
	clock = uint16(when.Second()/2 | when.Minute()<<5 | when.Hour()<<11)         // #nosec G115 -- 5, 6 and 5 bits

	return date, clock
}

func (c *creator) noteTime(t time.Time) {
	if t.After(c.latest) {
		c.latest = t
	}
}

// flush prints the pending entry's line, now that its sizes are known.
func (c *creator) flush() {
	if c.pending == nil {
		return
	}

	header := c.pending.header

	if !c.opts.quiet {
		method, saved := "stored", uint64(0)
		if header.Method == zip.Deflate && header.UncompressedSize64 > 0 {
			method = "deflated"
			// Info-ZIP's rounding, through the whole uncompressed size.
			saved = (percent*(header.UncompressedSize64-min(header.CompressedSize64, header.UncompressedSize64)) +
				header.UncompressedSize64/2) / header.UncompressedSize64
		}

		fmt.Fprintf(c.stdout, "%8s: %s (%s %d%%)\n", c.pending.verb, header.Name, method, saved)
	}

	c.pending = nil
}

// touch is -o with nothing to add: the archive takes its latest entry's time.
func (c *creator) touch(target string, existing *zip.ReadCloser) int {
	for _, file := range existing.File {
		c.noteTime(file.Modified)
	}

	if c.latest.IsZero() {
		return ExitOK
	}

	if err := os.Chtimes(target, c.latest, c.latest); err != nil {
		return c.errorf(ExitCreate, "Could not set time of %s (%v)", target, err)
	}

	return ExitOK
}

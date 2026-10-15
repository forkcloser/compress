package unzipcmd

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/forkcloser/compress/internal/constants"
)

// Exit statuses, Info-ZIP's.
const (
	ExitOK      = 0
	ExitWarning = 1
	ExitError   = 2
	ExitArchive = 9
	ExitUsage   = 10
	ExitNoMatch = 11
)

// Run is the unzip command: args follow the command name.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	opts, err := parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "unzip:  %v\n%s", err, helpText)

		return ExitUsage
	}

	if opts.help {
		fmt.Fprintf(stdout, "unzip (forkcloser/compress) %s\n%s", version, helpText)

		return ExitOK
	}

	if opts.archive == "" {
		fmt.Fprint(stderr, helpText)

		return ExitUsage
	}

	name, reader, status := open(opts.archive, stdout, stderr, opts.quiet || opts.pipe)
	if reader == nil {
		return status
	}

	defer func() { _ = reader.Close() }()

	files, status := selectFiles(opts, reader.File, stdout)
	if status != ExitOK && len(files) == 0 {
		return status
	}

	switch {
	case opts.list:
		return list(files, stdout)
	case opts.test:
		return test(name, files, stdout, opts.quiet)
	case opts.pipe:
		return pipe(files, stdout, stderr)
	default:
		return extract(opts, files, stdout, stderr, status)
	}
}

// open finds the archive, under its name or with ".zip" added as Info-ZIP
// tries, and announces it unless quiet. A missing or unreadable archive is
// Info-ZIP's status 9.
func open(archive string, stdout, stderr io.Writer, quiet bool) (string, *zip.ReadCloser, int) {
	name := archive

	reader, err := zip.OpenReader(name)
	if errors.Is(err, fs.ErrNotExist) && !strings.HasSuffix(strings.ToLower(name), ".zip") {
		name += ".zip"
		reader, err = zip.OpenReader(name)
	}

	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "unzip:  cannot find or open %s, %s.zip or %s.ZIP.\n", archive, archive, archive)

		return "", nil, ExitArchive
	}

	if err != nil {
		fmt.Fprintf(stdout, "Archive:  %s\n", name)
		fmt.Fprintf(stderr, "  End-of-central-directory signature not found.  Either this file is not\n"+
			"  a zipfile, or it constitutes one disk of a multi-part archive.\n"+
			"unzip:  cannot find zipfile directory in %s (%v).\n", name, err)

		return "", nil, ExitArchive
	}

	if !quiet {
		fmt.Fprintf(stdout, "Archive:  %s\n", name)
	}

	return name, reader, ExitOK
}

// selectFiles is the entries to work on: those matching a member pattern
// (all, with none given) and no exclude. A pattern matching nothing is
// Info-ZIP's caution; none matching anything is its status 11.
func selectFiles(opts *options, all []*zip.File, stdout io.Writer) ([]*zip.File, int) {
	matched := make([]bool, len(opts.members))

	var files []*zip.File

	for _, file := range all {
		if excluded(opts.excludes, file.Name) {
			continue
		}

		if len(opts.members) == 0 {
			files = append(files, file)

			continue
		}

		for i, pattern := range opts.members {
			if match(pattern, file.Name) {
				matched[i] = true

				files = append(files, file)

				break
			}
		}
	}

	status := ExitOK

	for i, pattern := range opts.members {
		if !matched[i] {
			fmt.Fprintf(stdout, "caution: filename not matched:  %s\n", pattern)

			status = ExitNoMatch
		}
	}

	return files, status
}

func excluded(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if match(pattern, name) {
			return true
		}
	}

	return false
}

// extract writes the selected entries under the directory, creating it.
func extract(opts *options, files []*zip.File, stdout, stderr io.Writer, status int) int {
	dir := opts.exdir
	if dir == "" {
		dir = "."
	}

	if err := os.MkdirAll(dir, constants.DirPermissions); err != nil {
		fmt.Fprintf(stderr, "checkdir error:  cannot create %s\n", dir)

		return ExitError
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		fmt.Fprintf(stderr, "checkdir error:  cannot open %s\n", dir)

		return ExitError
	}

	defer func() { _ = root.Close() }()

	run := &extractor{opts: opts, root: root, stdout: stdout, stderr: stderr, status: status}

	for _, file := range files {
		run.entry(file)
	}

	run.finishDirs()

	return run.status
}

package unzipcmd

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/forkcloser/go-compress/internal/constants"
)

// Exit statuses, Info-ZIP's.
const (
	ExitOK          = 0
	ExitWarning     = 1
	ExitError       = 2
	ExitPassword    = 5
	ExitArchive     = 9
	ExitUsage       = 10
	ExitNoMatch     = 11
	ExitUnsupported = 81
)

// worse is the status a run ends with out of two, as Info-ZIP settles it:
// an error by its value, over a pattern that matched nothing, over an entry
// skipped for its method, over a warning.
func worse(one, other int) int {
	severity := []int{ExitOK, ExitWarning, ExitUnsupported, ExitNoMatch, ExitError, ExitPassword}

	if slices.Index(severity, other) > slices.Index(severity, one) {
		return other
	}

	return one
}

// Run is the unzip command: args follow the command name.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	opts, err := parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "unzip:  %v\n%s", err, helpText)

		return ExitUsage
	}

	if opts.help {
		fmt.Fprintf(stdout, "unzip (forkcloser/go-compress) %s\n%s", version, helpText)

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

	files, unmatched := selectFiles(opts, reader.File)

	// Members, and none of them left to work on: Info-ZIP's status 11, with
	// no caution when each pattern did match a member that an exclude took.
	left := ExitOK
	if len(files) == 0 && len(reader.File) > 0 {
		left = ExitNoMatch
	}

	switch {
	case opts.list:
		// A listing says nothing of a pattern.
		return worse(list(files, stdout), left)
	case opts.test:
		return test(name, files, unmatched, left, stdout, opts.quiet)
	case opts.pipe:
		return cautions(stderr, unmatched, worse(pipe(files, stdout, stderr), left))
	default:
		return cautions(stderr, unmatched, worse(extract(opts, files, stdout, stderr), left))
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
// (all, with none given) and no exclude, and the patterns that matched
// nothing. A pattern has matched once a member bears its name, excluded or
// not: Info-ZIP looks at the excludes after.
func selectFiles(opts *options, all []*zip.File) ([]*zip.File, []string) {
	matched := make([]bool, len(opts.members))

	var files []*zip.File

	for _, file := range all {
		wanted := len(opts.members) == 0

		for i, pattern := range opts.members {
			if match(pattern, file.Name) {
				matched[i] = true
				wanted = true

				break
			}
		}

		if wanted && !excluded(opts.excludes, file.Name) {
			files = append(files, file)
		}
	}

	var unmatched []string

	for i, pattern := range opts.members {
		if !matched[i] {
			unmatched = append(unmatched, pattern)
		}
	}

	return files, unmatched
}

// cautions writes Info-ZIP's line for each pattern that matched nothing,
// once the entries are done as it does, and is the run's status with them:
// 11, unless an error is worse.
func cautions(out io.Writer, unmatched []string, status int) int {
	for _, pattern := range unmatched {
		fmt.Fprintf(out, "caution: filename not matched:  %s\n", pattern)

		status = worse(status, ExitNoMatch)
	}

	return status
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
func extract(opts *options, files []*zip.File, stdout, stderr io.Writer) int {
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

	run := &extractor{opts: opts, root: root, stdout: stdout, stderr: stderr}

	for _, file := range files {
		run.entry(file)
	}

	run.finishDirs()

	return run.status
}

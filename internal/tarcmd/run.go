package tarcmd

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Exit statuses, GNU tar's.
const (
	ExitOK    = 0
	ExitError = 2
	ExitUsage = 64
)

const helpText = `Usage: tar [OPTION...] [FILE]...
A tar archiver for the rig: GNU tar's grammar for the options the rig uses.

Main operation mode:
  -c, --create               create a new archive
  -t, --list                 list the contents of an archive
  -x, --extract, --get       extract files from an archive

Common options:
  -f, --file=ARCHIVE         use archive file ARCHIVE ("-" for standard input or output)
  -C, --directory=DIR        change to directory DIR
  -v, --verbose              verbosely list files processed
  -T, --files-from=FILE      get names to extract or create from FILE
      --strip-components=N   strip N leading components from file names on extraction
      --no-recursion         avoid descending automatically in directories
      --sort=ORDER           directory sorting order: none (default) or name
      --exclude=PATTERN      exclude files matching PATTERN, a wildcard
  -P, --absolute-names       don't strip leading '/'s or refuse '..' in file names

Compression:
  -a, --auto-compress        use archive suffix to determine the compression program
  -j, --bzip2                filter the archive through bzip2 (reading only)
  -J, --xz                   filter the archive through xz
  -z, --gzip                 filter the archive through gzip
      --zstd                 filter the archive through zstd
      --lz4                  filter the archive through lz4
  -I, --use-compress-program=PROG
                             filter through PROG (must accept -d); a known
                             compressor (zstd, zstdmt, unzstd, gzip, xz, ...)
                             is its built-in codec

Attributes:
      --format=FORMAT        archive format: ustar, posix (pax) or gnu
      --posix                same as --format=posix
      --group=NAME[:GID]     force NAME as group for added files
      --owner=NAME[:UID]     force NAME as owner for added files
      --mtime=DATE           set mtime for added files from DATE
      --numeric-owner        always use numbers for user/group names
  -m, --touch                don't extract file modified time
  -p, --preserve-permissions, --same-permissions
                             extract information about file permissions
      --no-same-permissions  apply the user's umask when extracting permissions

Other:
      --help                 give this help
      --version              print program version
`

// Run is the tar command: args are the arguments after the command name,
// stdin and stdout are the archive when -f names "-" or is absent, stderr
// takes the diagnostics. The result is the exit status.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	opts, err := parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "tar: %v\nTry 'tar --help' for more information.\n", err)

		return ExitUsage
	}

	if opts.help {
		fmt.Fprint(stdout, helpText)

		return ExitOK
	}

	if opts.version {
		fmt.Fprintf(stdout, "tar (forkcloser/compress) %s\n", version)

		return ExitOK
	}

	var failed bool

	switch opts.mode {
	case modeCreate:
		failed, err = create(opts, stdout, stderr)
	case modeExtract:
		failed, err = extract(opts, stdin, stdout, stderr)
	case modeList:
		err = list(opts, stdin, stdout)
	case modeNone:
	}

	if err != nil {
		fmt.Fprintf(stderr, "tar: %v\ntar: Error is not recoverable: exiting now\n", err)

		return ExitError
	}

	if failed {
		fmt.Fprintln(stderr, "tar: Exiting with failure status due to previous errors")

		return ExitError
	}

	return ExitOK
}

// createWrap is the compression a created archive gets: the option's, or the
// name's under -a, or none.
func createWrap(opts *options) compression {
	if opts.compression != compressAuto {
		return opts.compression
	}

	if opts.autoCompress && opts.archive != "" && opts.archive != "-" {
		return bySuffix(opts.archive)
	}

	return compressNone
}

// create writes the archive to the -f file, or to stdout.
func create(opts *options, stdout, stderr io.Writer) (bool, error) {
	out, verbose := stdout, stderr // on stdout, the archive; the names go where they do not corrupt it

	var file *os.File

	if opts.archive != "" && opts.archive != "-" {
		created, err := os.Create(opts.archive) // #nosec G304 -- the archive is the command's own -f argument
		if err != nil {
			return false, fmt.Errorf("%s: Cannot open: %w", opts.archive, errReason(err))
		}

		defer func() { _ = created.Close() }() // the explicit close below is the one that reports

		file, out, verbose = created, created, stdout
	}

	compressor, err := openWriter(createWrap(opts), opts.program, out)
	if err != nil {
		return false, err
	}

	archiver := &creator{opts: opts, writer: tar.NewWriter(compressor), verbose: verbose, stderr: stderr}

	for _, m := range opts.members {
		if err := archiver.addMember(m); err != nil {
			return archiver.failed, err
		}
	}

	if err := archiver.writer.Close(); err != nil {
		return archiver.failed, fmt.Errorf("Cannot write: %w", err)
	}

	if err := compressor.Close(); err != nil {
		return archiver.failed, fmt.Errorf("Cannot write: %w", err)
	}

	if file != nil {
		if err := file.Close(); err != nil {
			return archiver.failed, fmt.Errorf("%s: Cannot close: %w", opts.archive, errReason(err))
		}
	}

	return archiver.failed, nil
}

// openArchive opens the archive to read, -f's file or stdin, through the
// decompressor the options or the magic name. The second result releases
// the file, when there is one.
func openArchive(opts *options, stdin io.Reader) (io.ReadCloser, func(), error) {
	source := stdin
	release := func() {}

	if opts.archive != "" && opts.archive != "-" {
		file, err := os.Open(opts.archive) // #nosec G304 -- the archive is the command's own -f argument
		if err != nil {
			return nil, nil, fmt.Errorf("%s: Cannot open: %w", opts.archive, errReason(err))
		}

		source = file
		release = func() { _ = file.Close() } // read-only: a close error carries nothing
	}

	reader, err := openReader(opts.compression, opts.program, source)
	if err != nil {
		release()

		return nil, nil, fmt.Errorf("%s: %w", opts.archive, err)
	}

	return reader, release, nil
}

func extract(opts *options, stdin io.Reader, stdout, stderr io.Writer) (bool, error) {
	reader, release, err := openArchive(opts, stdin)
	if err != nil {
		return false, err
	}

	defer release()

	rootDir, base, err := extractionRoot(opts)
	if err != nil {
		return false, err
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return false, fmt.Errorf("%s: Cannot open: %w", rootDir, errReason(err))
	}

	defer func() { _ = root.Close() }() // a directory handle: a close error carries nothing

	unpacker := &extractor{opts: opts, root: root, base: base, stdout: stdout, stderr: stderr}

	if err := unpacker.extract(tar.NewReader(reader)); err != nil {
		return unpacker.failed, err
	}

	if err := reader.Close(); err != nil && !errors.Is(err, io.EOF) {
		return unpacker.failed, fmt.Errorf("%s: %w", opts.archive, err)
	}

	return unpacker.failed, nil
}

// extractionRoot is the directory extraction is confined to: -C's, or under
// -P the file system root of -C's volume, the names then resolved against
// -C's absolute path (the second result).
func extractionRoot(opts *options) (rootDir, base string, err error) {
	if !opts.absoluteNames {
		return opts.dir, "", nil
	}

	base, err = filepath.Abs(opts.dir)
	if err != nil {
		return "", "", fmt.Errorf("%s: Cannot open: %w", opts.dir, errReason(err))
	}

	return filepath.VolumeName(base) + string(filepath.Separator), base, nil
}

func list(opts *options, stdin io.Reader, stdout io.Writer) error {
	reader, release, err := openArchive(opts, stdin)
	if err != nil {
		return err
	}

	defer release()

	printer := &lister{opts: opts, stdout: stdout}

	if err := printer.list(tar.NewReader(reader)); err != nil {
		return err
	}

	if err := reader.Close(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", opts.archive, err)
	}

	return nil
}

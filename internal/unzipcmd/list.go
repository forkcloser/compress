package unzipcmd

import (
	"archive/zip"
	"fmt"
	"io"
)

// list is -l: Info-ZIP's short listing, the DOS wall clock as stored.
func list(files []*zip.File, stdout io.Writer) int {
	fmt.Fprint(stdout, "  Length      Date    Time    Name\n---------  ---------- -----   ----\n")

	var total uint64

	for _, file := range files {
		when := file.Modified
		total += file.UncompressedSize64

		fmt.Fprintf(stdout, "%9d  %02d-%02d-%04d %02d:%02d   %s\n", file.UncompressedSize64,
			when.Month(), when.Day(), when.Year(), when.Hour(), when.Minute(), file.Name)
	}

	fmt.Fprint(stdout, "---------                     -------\n")
	fmt.Fprintf(stdout, "%9d                     %d file%s\n", total, len(files), plural(len(files)))

	return ExitOK
}

// test is -t: every entry read through, its CRC checked at its end. An
// entry skipped for its method is no error: it is counted apart from the
// entries tested, and the status says so once nothing worse did. A pattern
// that matched nothing is an error here, in the summary's words; left is
// the status when the patterns and excludes left nothing to test.
func test(name string, files []*zip.File, unmatched []string, left int, stdout io.Writer, quiet bool) int {
	status := ExitOK
	tested, unsupported := 0, 0

	for _, file := range files {
		err := discard(file)
		skip := skipped(stdout, file.Name, err, quiet)

		switch {
		case skip == ExitUnsupported:
			unsupported++
		case skip != ExitOK:
			status = worse(status, skip)
		case err != nil:
			status = worse(status, ExitError)

			fmt.Fprintf(stdout, "%11s: %-24s %v\n", "testing", file.Name, err)
		default:
			tested++

			if !quiet {
				fmt.Fprintf(stdout, "%11s: %-24s OK\n", "testing", file.Name)
			}
		}
	}

	status = cautions(stdout, unmatched, status)

	switch {
	case status == ExitOK && left != ExitOK:
		fmt.Fprintf(stdout, "Caution:  zero files tested in %s.\n", name)

		status = left
	case status != ExitOK:
		fmt.Fprintf(stdout, "At least one error was detected in %s.\n", name)
	case unsupported == 0:
		fmt.Fprintf(stdout, "No errors detected in compressed data of %s.\n", name)
	case tested == 0:
		fmt.Fprintf(stdout, "Caution:  zero files tested in %s.\n", name)
	default:
		fmt.Fprintf(stdout, "No errors detected in %s for the %d file%s tested.\n", name, tested, plural(tested))
	}

	if unsupported > 0 {
		fmt.Fprintf(stdout, "%d file%s skipped because of unsupported compression or encoding.\n",
			unsupported, plural(unsupported))

		status = worse(status, ExitUnsupported)
	}

	return status
}

func plural(count int) string {
	if count == 1 {
		return ""
	}

	return "s"
}

func discard(file *zip.File) error {
	return copyContent(io.Discard, file)
}

// pipe is -p: the files' content to standard output, nothing else; a
// damaged entry is reported in the words extraction and -t use.
func pipe(files []*zip.File, stdout, stderr io.Writer) int {
	status := ExitOK

	for _, file := range files {
		if file.Mode().IsDir() {
			continue
		}

		err := copyContent(stdout, file)
		if err == nil {
			continue
		}

		// -p is quiet: the line for a method is not written.
		if skip := skipped(stderr, file.Name, err, true); skip != ExitOK {
			status = worse(status, skip)

			continue
		}

		fmt.Fprintf(stderr, "%s:  %v\n", file.Name, err)

		status = worse(status, ExitError)
	}

	return status
}

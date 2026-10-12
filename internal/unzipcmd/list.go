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

	plural := "s"
	if len(files) == 1 {
		plural = ""
	}

	fmt.Fprint(stdout, "---------                     -------\n")
	fmt.Fprintf(stdout, "%9d                     %d file%s\n", total, len(files), plural)

	return ExitOK
}

// test is -t: every entry read through, its CRC checked at its end.
func test(name string, files []*zip.File, stdout io.Writer, quiet bool) int {
	failed := false

	for _, file := range files {
		err := discard(file)

		switch {
		case err != nil:
			failed = true

			fmt.Fprintf(stdout, "%11s: %-24s %v\n", "testing", file.Name, err)
		case !quiet:
			fmt.Fprintf(stdout, "%11s: %-24s OK\n", "testing", file.Name)
		}
	}

	if failed {
		fmt.Fprintf(stdout, "At least one error was detected in %s.\n", name)

		return ExitError
	}

	fmt.Fprintf(stdout, "No errors detected in compressed data of %s.\n", name)

	return ExitOK
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

		if err := copyContent(stdout, file); err != nil {
			fmt.Fprintf(stderr, "%s:  %v\n", file.Name, err)

			status = ExitError
		}
	}

	return status
}

package zipcmd

import (
	"fmt"
	"io"
)

// Exit statuses, Info-ZIP's.
const (
	ExitOK        = 0
	ExitStructure = 3
	ExitNothing   = 12
	ExitCreate    = 15
	ExitUsage     = 16
	ExitInput     = 18
)

// Run is the zip command: args follow the command name.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	opts, err := parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "\nzip error: %v\n", err)

		return ExitUsage
	}

	if opts.help {
		fmt.Fprint(stdout, helpText)

		return ExitOK
	}

	// -v with no archive is the version, with one it is verbosity.
	if opts.version || (opts.verbose && opts.archive == "") {
		fmt.Fprintf(stdout, "zip (forkcloser/compress) %s\n", version)

		return ExitOK
	}

	if opts.archive == "" {
		fmt.Fprint(stderr, helpText)

		return ExitUsage
	}

	if opts.namesStdin {
		names, err := readNames(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "\nzip error: %v\n", err)

			return ExitInput
		}

		opts.inputs = append(opts.inputs, names...)
	}

	c := &creator{opts: opts, stdout: stdout, stderr: stderr}

	return c.run()
}

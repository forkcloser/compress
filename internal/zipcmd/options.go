package zipcmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

const defaultLevel = 6

const helpText = `Usage: zip [-options] [zipfile [file ...]] [-@]
  The default action is to add or replace zipfile entries from the files named.
  -r   recurse into directories          -j   junk (don't record) directory names
  -0   store only                        -1   compress faster
  -9   compress better                   -q   quiet operation
  -X   eXclude eXtra file attributes     -D   do not add directory entries
  -@   read names from stdin             -o   make zipfile as old as latest entry
  -v   version info (with no zipfile)    -h   show this help
`

var errUsage = errors.New("Invalid command arguments")

type options struct {
	recurse      bool
	quiet        bool
	stripExtra   bool
	namesStdin   bool
	latestTime   bool
	junkPaths    bool
	noDirEntries bool
	verbose      bool
	help         bool
	version      bool
	level        int
	archive      string
	inputs       []string
}

// parse reads the options and operands, which Info-ZIP lets interleave: the
// first operand is the archive, the rest are inputs.
func parse(args []string) (*options, error) {
	opts := &options{level: defaultLevel}

	var operands []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--":
			operands = append(operands, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			if err := opts.long(arg[2:]); err != nil {
				return nil, err
			}
		case len(arg) > 1 && arg[0] == '-':
			if err := opts.cluster(arg[1:]); err != nil {
				return nil, err
			}
		default:
			operands = append(operands, arg)
		}
	}

	if len(operands) > 0 {
		opts.archive, opts.inputs = operands[0], operands[1:]
	}

	return opts, nil
}

func (o *options) cluster(letters string) error {
	for _, letter := range letters {
		switch letter {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			o.level = int(letter - '0')
		case 'r':
			o.recurse = true
		case 'q':
			o.quiet = true
		case 'X':
			o.stripExtra = true
		case '@':
			o.namesStdin = true
		case 'o':
			o.latestTime = true
		case 'j':
			o.junkPaths = true
		case 'D':
			o.noDirEntries = true
		case 'v':
			o.verbose = true
		case 'h':
			o.help = true
		default:
			return fmt.Errorf("%w (short option '%c' not supported)", errUsage, letter)
		}
	}

	return nil
}

func (o *options) long(name string) error {
	switch name {
	case "recurse-paths":
		o.recurse = true
	case "quiet":
		o.quiet = true
	case "strip-extra":
		o.stripExtra = true
	case "names-stdin":
		o.namesStdin = true
	case "latest-time":
		o.latestTime = true
	case "junk-paths":
		o.junkPaths = true
	case "no-dir-entries":
		o.noDirEntries = true
	case "help":
		o.help = true
	case "version":
		o.version = true
	default:
		return fmt.Errorf("%w (long option '%s' not supported)", errUsage, name)
	}

	return nil
}

// readNames is -@: one input per line of standard input, blank lines skipped.
func readNames(stdin io.Reader) ([]string, error) {
	var names []string

	scanner := bufio.NewScanner(stdin)
	for scanner.Scan() {
		if name := strings.TrimRight(scanner.Text(), "\r"); name != "" {
			names = append(names, name)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading names from stdin: %w", err)
	}

	return names, nil
}

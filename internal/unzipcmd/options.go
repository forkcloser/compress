package unzipcmd

import (
	"errors"
	"fmt"
	"strings"
)

const helpText = `Usage: unzip [-opts[modifiers]] file[.zip] [list] [-x xlist] [-d exdir]
  Default action is to extract files in list, except those in xlist, to exdir;
  file[.zip] may be a wildcard.
  -l  list files (short format)      -p  extract files to pipe, no messages
  -t  test compressed archive data   -x  exclude files that follow (in xlist)
  -d  extract files into exdir       -h  show this help
modifiers:
  -n  never overwrite existing files -q  quiet mode
  -o  overwrite files WITHOUT prompting -j  junk paths (do not make directories)
`

var errUsage = errors.New("invalid option")

type options struct {
	quiet     bool
	overwrite bool
	never     bool
	list      bool
	test      bool
	pipe      bool
	junk      bool
	help      bool
	exdir     string
	archive   string
	members   []string
	excludes  []string
}

// parse reads the options and operands, which interleave: the first operand
// is the archive, the rest are members, or excludes once -x was seen.
func parse(args []string) (*options, error) {
	opts := &options{}
	excluding := false

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--help":
			opts.help = true
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("%w -- '%s'", errUsage, arg)
		case len(arg) > 1 && arg[0] == '-':
			consumed, err := opts.cluster(arg[1:], args[i+1:], &excluding)
			if err != nil {
				return nil, err
			}

			i += consumed
		case opts.archive == "":
			opts.archive = arg
		case excluding:
			opts.excludes = append(opts.excludes, arg)
		default:
			opts.members = append(opts.members, arg)
		}
	}

	return opts, nil
}

// cluster applies a run of short options; -d takes the rest of the cluster
// or the next word as the directory, and the result is how many following
// words were consumed.
func (o *options) cluster(letters string, following []string, excluding *bool) (int, error) {
	for i, letter := range letters {
		switch letter {
		case 'q':
			o.quiet = true
		case 'o':
			o.overwrite = true
		case 'n':
			o.never = true
		case 'l':
			o.list = true
		case 't':
			o.test = true
		case 'p':
			o.pipe = true
		case 'j':
			o.junk = true
		case 'h':
			o.help = true
		case 'x':
			*excluding = true
		case 'd':
			if rest := letters[i+1:]; rest != "" {
				o.exdir = rest

				return 0, nil
			}

			if len(following) == 0 {
				return 0, fmt.Errorf("%w: -d requires a directory", errUsage)
			}

			o.exdir = following[0]

			return 1, nil
		default:
			return 0, fmt.Errorf("%w -- '%c'", errUsage, letter)
		}
	}

	return 0, nil
}

// match is Info-ZIP's wildcard match: "*" is any run of characters, "/"
// included, "?" any one character.
func match(pattern, name string) bool {
	for pattern != "" {
		switch pattern[0] {
		case '*':
			return matchStar(strings.TrimLeft(pattern, "*"), name)
		case '?':
			if name == "" {
				return false
			}
		default:
			if name == "" || name[0] != pattern[0] {
				return false
			}
		}

		pattern, name = pattern[1:], name[1:]
	}

	return name == ""
}

// matchStar matches what follows a "*" against every suffix of name.
func matchStar(rest, name string) bool {
	if rest == "" {
		return true
	}

	for i := range len(name) + 1 {
		if match(rest, name[i:]) {
			return true
		}
	}

	return false
}

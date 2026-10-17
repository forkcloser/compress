package tarcmd

import (
	"archive/tar"
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// mode is what the invocation does with the archive.
type mode int

const (
	modeNone mode = iota
	modeCreate
	modeExtract
	modeList
)

// compression names the stream wrapped around the archive. compressAuto is
// "by the archive's name" on create and "by its magic" on read.
type compression int

const (
	compressAuto compression = iota
	compressNone
	compressGzip
	compressXz
	compressBzip2
	compressZstd
	compressLz4
	// compressProgram is an --use-compress-program the command does not
	// know: the archive is filtered through it, as GNU tar does.
	compressProgram
)

// member is a name given on the command line or read from a -T list, with
// the -C directory in effect where it appeared.
type member struct {
	dir  string
	name string
	// list is the -T file the names come from; name is empty then.
	list string
}

// ownerSpec is --owner or --group: a name, an id, or both.
type ownerSpec struct {
	name string
	id   int
}

// options is a parsed invocation.
type options struct {
	mode            mode
	archive         string // "" when not given, "-" for standard input or output
	verbose         bool
	compression     compression
	autoCompress    bool
	stripComponents int
	samePermissions bool
	touch           bool // -m: do not restore modification times
	format          tar.Format
	owner           *ownerSpec
	group           *ownerSpec
	numericOwner    bool
	mtime           *time.Time
	sortNames       bool
	noRecursion     bool
	absoluteNames   bool     // -P: names keep their leading slash and their ".."
	excludes        []string // --exclude patterns
	program         string   // --use-compress-program, when not a known compressor
	dir             string   // the last -C, for extract and list
	members         []member // in order: what create archives, what extract and list take
	help            bool
	version         bool
}

// usageError is a command line tar cannot act on; the exit status is 64.
type usageError struct {
	msg string
}

func (e *usageError) Error() string {
	return e.msg
}

func usage(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// takesArgument are the short options that consume a value.
const takesArgument = "fCTI"

// parse reads args (without the command name) into options. Besides a
// usageError, it returns no error.
func parse(args []string) (*options, error) {
	opts := &options{dir: "."}

	args = expandOldStyle(args)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--":
			for _, name := range args[i+1:] {
				opts.addMember(name)
			}

			return opts.finish()
		case strings.HasPrefix(arg, "--"):
			consumed, err := opts.long(arg[2:], args[i+1:])
			if err != nil {
				return nil, err
			}

			i += consumed
		case len(arg) > 1 && arg[0] == '-':
			consumed, err := opts.cluster(arg[1:], args[i+1:])
			if err != nil {
				return nil, err
			}

			i += consumed
		default:
			opts.addMember(arg)
		}
	}

	return opts.finish()
}

// expandOldStyle rewrites a first argument that is an option bundle without a
// dash (xf, cvzf) into short options, each letter that takes a value taking
// the next positional argument, in the bundle's order.
func expandOldStyle(args []string) []string {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return args
	}

	bundle, rest := args[0], args[1:]
	out := make([]string, 0, len(args)+len(bundle))

	for _, letter := range bundle {
		out = append(out, "-"+string(letter))

		if strings.ContainsRune(takesArgument, letter) && len(rest) > 0 {
			out = append(out, rest[0])
			rest = rest[1:]
		}
	}

	return append(out, rest...)
}

// cluster applies a run of short options (the letters after one dash); an
// option that takes a value takes the rest of the cluster or the next word.
// It returns how many following words it consumed.
func (o *options) cluster(letters string, following []string) (int, error) {
	for i, letter := range letters {
		if !strings.ContainsRune(takesArgument, letter) {
			if err := o.short(letter); err != nil {
				return 0, err
			}

			continue
		}

		value := letters[i+1:]
		if value != "" {
			return 0, o.shortWithValue(letter, value)
		}

		if len(following) == 0 {
			return 0, usage("option requires an argument -- '%c'", letter)
		}

		return 1, o.shortWithValue(letter, following[0])
	}

	return 0, nil
}

func (o *options) short(letter rune) error {
	switch letter {
	case 'c':
		return o.setMode(modeCreate)
	case 'x':
		return o.setMode(modeExtract)
	case 't':
		return o.setMode(modeList)
	case 'v':
		o.verbose = true
	case 'z':
		o.compression = compressGzip
	case 'J':
		o.compression = compressXz
	case 'j':
		o.compression = compressBzip2
	case 'a':
		o.autoCompress = true
	case 'p':
		o.samePermissions = true
	case 'm':
		o.touch = true
	case 'P':
		o.absoluteNames = true
	case 'h':
		return usage("option -h (--dereference) is not supported")
	case 'O':
		return usage("option -O (--to-stdout) is not supported")
	default:
		return usage("invalid option -- '%c'", letter)
	}

	return nil
}

func (o *options) shortWithValue(letter rune, value string) error {
	switch letter {
	case 'f':
		o.archive = value
	case 'C':
		o.changeDir(value)
	case 'T':
		o.members = append(o.members, member{dir: o.dir, list: value})
	case 'I':
		return o.setCompressProgram(value)
	default:
		return usage("invalid option -- '%c'", letter)
	}

	return nil
}

// long applies one long option, given without its dashes, with its value
// after '=' or in the next word. It returns how many following words it
// consumed.
func (o *options) long(spec string, following []string) (int, error) {
	name, value, hasValue := strings.Cut(spec, "=")

	flag, isFlag := longFlags()[name]
	if isFlag {
		if hasValue {
			return 0, usage("option '--%s' doesn't allow an argument", name)
		}

		return 0, flag(o)
	}

	withValue, takesValue := longWithValue()[name]
	if !takesValue {
		return 0, usage("unrecognized option '--%s'", name)
	}

	if hasValue {
		return 0, withValue(o, value)
	}

	if len(following) == 0 {
		return 0, usage("option '--%s' requires an argument", name)
	}

	return 1, withValue(o, following[0])
}

// setFlag is a long option without a value: it sets one field.
func setFlag(set func(*options)) func(*options) error {
	return func(opts *options) error {
		set(opts)

		return nil
	}
}

func longFlags() map[string]func(*options) error {
	return map[string]func(*options) error{
		"create":               func(opts *options) error { return opts.setMode(modeCreate) },
		"extract":              func(opts *options) error { return opts.setMode(modeExtract) },
		"get":                  func(opts *options) error { return opts.setMode(modeExtract) },
		"list":                 func(opts *options) error { return opts.setMode(modeList) },
		"verbose":              setFlag(func(opts *options) { opts.verbose = true }),
		"gzip":                 setFlag(func(opts *options) { opts.compression = compressGzip }),
		"gunzip":               setFlag(func(opts *options) { opts.compression = compressGzip }),
		"ungzip":               setFlag(func(opts *options) { opts.compression = compressGzip }),
		"xz":                   setFlag(func(opts *options) { opts.compression = compressXz }),
		"bzip2":                setFlag(func(opts *options) { opts.compression = compressBzip2 }),
		"zstd":                 setFlag(func(opts *options) { opts.compression = compressZstd }),
		"lz4":                  setFlag(func(opts *options) { opts.compression = compressLz4 }),
		"auto-compress":        setFlag(func(opts *options) { opts.autoCompress = true }),
		"same-permissions":     setFlag(func(opts *options) { opts.samePermissions = true }),
		"preserve-permissions": setFlag(func(opts *options) { opts.samePermissions = true }),
		"no-same-permissions":  setFlag(func(opts *options) { opts.samePermissions = false }),
		"same-owner":           setFlag(func(*options) {}),
		"no-same-owner":        setFlag(func(*options) {}),
		"touch":                setFlag(func(opts *options) { opts.touch = true }),
		"numeric-owner":        setFlag(func(opts *options) { opts.numericOwner = true }),
		"no-recursion":         setFlag(func(opts *options) { opts.noRecursion = true }),
		"recursion":            setFlag(func(opts *options) { opts.noRecursion = false }),
		"absolute-names":       setFlag(func(opts *options) { opts.absoluteNames = true }),
		"posix":                setFlag(func(opts *options) { opts.format = tar.FormatPAX }),
		// Directory modes and times are always applied last, so the delay
		// is what extraction already does; there are no remote archives,
		// so a colon in a name is always local.
		"delay-directory-restore":    setFlag(func(*options) {}),
		"no-delay-directory-restore": setFlag(func(*options) {}),
		"force-local":                setFlag(func(*options) {}),
		"help":                       setFlag(func(opts *options) { opts.help = true }),
		"version":                    setFlag(func(opts *options) { opts.version = true }),
	}
}

func longWithValue() map[string]func(*options, string) error {
	return map[string]func(*options, string) error{
		"file":                 func(opts *options, value string) error { opts.archive = value; return nil },
		"directory":            func(opts *options, value string) error { opts.changeDir(value); return nil },
		"files-from":           (*options).addList,
		"strip-components":     (*options).setStripComponents,
		"format":               (*options).setFormat,
		"owner":                (*options).setOwner,
		"group":                (*options).setGroup,
		"mtime":                (*options).setMtime,
		"sort":                 (*options).setSort,
		"exclude":              (*options).addExclude,
		"use-compress-program": (*options).setCompressProgram,
	}
}

func (o *options) addList(file string) error {
	o.members = append(o.members, member{dir: o.dir, list: file})

	return nil
}

func (o *options) addExclude(pattern string) error {
	o.excludes = append(o.excludes, pattern)

	return nil
}

// setCompressProgram is --use-compress-program: a compressor the command
// knows by name (zstd, zstdmt, unzstd, gzip, xz, …) is its own codec, its
// arguments being tuning that changes nothing in the stream; any other
// program is run as a filter.
func (o *options) setCompressProgram(program string) error {
	fields := strings.Fields(program)
	if len(fields) == 0 {
		return usage("option '--use-compress-program' requires a program")
	}

	if known, ok := knownProgram(fields[0]); ok {
		o.compression = known
		o.program = ""

		return nil
	}

	o.compression = compressProgram
	o.program = program

	return nil
}

func (o *options) setStripComponents(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return usage("Invalid number of components to strip: '%s'", value)
	}

	o.stripComponents = n

	return nil
}

func (o *options) setFormat(value string) error {
	switch value {
	case "ustar":
		o.format = tar.FormatUSTAR
	case "posix", "pax":
		o.format = tar.FormatPAX
	case "gnu", "oldgnu":
		o.format = tar.FormatGNU
	default:
		return usage("unknown archive format '%s'", value)
	}

	return nil
}

func (o *options) setOwner(value string) error {
	spec, err := parseOwner(value, lookupUser)
	if err != nil {
		return err
	}

	o.owner = spec

	return nil
}

func (o *options) setGroup(value string) error {
	spec, err := parseOwner(value, lookupGroup)
	if err != nil {
		return err
	}

	o.group = spec

	return nil
}

func (o *options) setMtime(value string) error {
	when, err := parseDate(value)
	if err != nil {
		return usage("Invalid mtime: '%s'", value)
	}

	o.mtime = &when

	return nil
}

func (o *options) setSort(value string) error {
	switch value {
	case "none":
		o.sortNames = false
	case "name":
		o.sortNames = true
	default:
		return usage("unknown sort order '%s'", value)
	}

	return nil
}

func (o *options) setMode(m mode) error {
	if o.mode != modeNone && o.mode != m {
		return usage("You may not specify more than one '-Acdtrux', '--delete' or '--test-label' option")
	}

	o.mode = m

	return nil
}

// changeDir applies a -C: relative to the previous one, as a chdir would be.
func (o *options) changeDir(dir string) {
	if filepath.IsAbs(dir) {
		o.dir = dir
	} else {
		o.dir = filepath.Join(o.dir, dir)
	}
}

func (o *options) addMember(name string) {
	o.members = append(o.members, member{dir: o.dir, name: name})
}

// finish checks what the options together must satisfy.
func (o *options) finish() (*options, error) {
	if o.help || o.version {
		return o, nil
	}

	if o.mode == modeNone {
		return nil, usage("You must specify one of the '-Acdtrux', '--delete' or '--test-label' options")
	}

	if o.mode == modeCreate && len(o.members) == 0 {
		return nil, usage("Cowardly refusing to create an empty archive")
	}

	return o, nil
}

// parseOwner reads NAME, NAME:ID, :ID or a bare ID. A name without an id is
// resolved through lookup; a bare number is an id with no name.
func parseOwner(spec string, lookup func(name string) (int, error)) (*ownerSpec, error) {
	name, id, hasID := strings.Cut(spec, ":")

	if hasID {
		n, err := strconv.Atoi(id)
		if err != nil || n < 0 {
			return nil, usage("Invalid owner or group ID: '%s'", spec)
		}

		return &ownerSpec{name: name, id: n}, nil
	}

	if n, err := strconv.Atoi(name); err == nil && n >= 0 {
		return &ownerSpec{id: n}, nil
	}

	n, err := lookup(name)
	if err != nil {
		return nil, usage("Invalid owner or group: '%s'", spec)
	}

	return &ownerSpec{name: name, id: n}, nil
}

func lookupUser(name string) (int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf("user %s: %w", name, err)
	}

	return strconv.Atoi(u.Uid) //nolint:wrapcheck // a non-numeric uid is not a unix user
}

func lookupGroup(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("group %s: %w", name, err)
	}

	return strconv.Atoi(g.Gid) //nolint:wrapcheck // a non-numeric gid is not a unix group
}

var errBadDate = errors.New("unrecognized date")

// parseDate reads the forms --mtime takes: a date with an optional time, in
// the local time zone as GNU tar reads it, @ followed by seconds since the
// epoch, or RFC 3339.
func parseDate(text string) (time.Time, error) {
	if seconds, ok := strings.CutPrefix(text, "@"); ok {
		n, err := strconv.ParseInt(seconds, 10, 64)
		if err != nil {
			return time.Time{}, errBadDate
		}

		return time.Unix(n, 0), nil
	}

	local := time.Local //nolint:gosmopolitan // --mtime is a local date, as for GNU tar

	for _, layout := range []string{
		time.DateTime,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
		time.DateOnly,
	} {
		if when, err := time.ParseInLocation(layout, text, local); err == nil {
			return when, nil
		}
	}

	if when, err := time.Parse(time.RFC3339, text); err == nil {
		return when, nil
	}

	return time.Time{}, errBadDate
}

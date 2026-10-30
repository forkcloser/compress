package filecodec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/forkcloser/xos"

	"github.com/forkcloser/compress/internal/longopt"
)

// Exit statuses, the tools'.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitWarning = 2
)

// Format is one tool: what it is called, the suffixes it reads and writes,
// and its streams.
type Format struct {
	// Name prefixes every diagnostic and names the tool in --help.
	Name string
	// Suffix is what a compressed file's name ends with.
	Suffix string
	// Suffixes maps every compressed suffix the tool recognizes to what
	// replaces it on decompression ("" strips it): ".tgz" to ".tar".
	Suffixes map[string]string
	// Levels are the compression levels accepted; DefaultLevel is the one
	// without a -N.
	MinLevel, MaxLevel, DefaultLevel int
	// Keep is whether the tool leaves its inputs in place unless told
	// otherwise: zstd does, gzip and xz remove them unless --keep.
	Keep bool
	// Warning is the exit status after a warning alone: ExitWarning for
	// gzip and xz, ExitError for zstd, which has no status between the two.
	Warning int
	// Compress wraps w in the format's encoder at level; the header fields
	// name the original file where the format records it (gzip), empty for
	// standard input or --no-name.
	Compress func(w io.Writer, level int, h Header) (io.WriteCloser, error)
	// Decompress wraps r in the format's decoder.
	Decompress func(r io.Reader) (io.ReadCloser, error)
	// Extra are the tool's own options beyond the shared ones: a short
	// letter or a long name, each with whether it takes a value, applied
	// to the options or refused.
	Extra map[string]Extra
	// Abbreviations is whether a long option may be given as any unambiguous
	// prefix of its name, as getopt_long takes it for gzip and xz; zstd's
	// own parser refuses one.
	Abbreviations bool
	// Help is the --help text.
	Help string
}

// Header is what a format may record about the original file.
type Header struct {
	Name    string
	ModTime int64 // seconds since the epoch, 0 for none
}

// Extra is one tool-specific option.
type Extra struct {
	TakesValue bool
	// Apply records the option; a nil Apply accepts and ignores it.
	Apply func(o *Options, value string) error
}

// Options is a parsed invocation.
type Options struct {
	Decompress bool
	Stdout     bool
	Force      bool
	Keep       bool
	Quiet      bool
	Test       bool
	Level      int
	// NoName leaves the original's name and time out of the stream (gzip -n).
	NoName  bool
	Help    bool
	Version bool
	Files   []string
}

var (
	errUsage      = errors.New("usage")
	errIsTerminal = errors.New("compressed data not written to a terminal; use -f to force")
	errNotRegular = errors.New("not a regular file")
)

// Run is the command: name is how it was invoked (unxz and gunzip decompress,
// xzcat and zcat decompress to standard output), args follow it.
func (f *Format) Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	opts, err := f.parse(name, args)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\nTry `%s --help' for more information.\n", f.Name, err, f.Name)

		return ExitError
	}

	if opts.Help {
		fmt.Fprint(stdout, f.Help)

		return ExitOK
	}

	if opts.Version {
		fmt.Fprintf(stdout, "%s (forkcloser/compress) %s\n", f.Name, version)

		return ExitOK
	}

	// --test reads a compressed stream, whatever else was asked.
	if opts.Test {
		opts.Decompress = true
	}

	run := &runner{format: f, opts: opts, stdin: stdin, stdout: stdout, stderr: stderr}

	if len(opts.Files) == 0 {
		run.file("-")
	}

	for _, file := range opts.Files {
		run.file(file)
	}

	return run.status
}

// parse reads the shared options, the tool's extras, and the levels.
func (f *Format) parse(name string, args []string) (*Options, error) {
	opts := &Options{Level: f.DefaultLevel, Keep: f.Keep}

	// unxz, gunzip and uncompress decompress; xzcat and zcat decompress to
	// standard output.
	base := strings.TrimSuffix(filepath.Base(name), ".exe")

	switch {
	case strings.HasPrefix(base, "un"), strings.HasPrefix(base, "gun"):
		opts.Decompress = true
	case strings.HasSuffix(base, "cat"):
		opts.Decompress, opts.Stdout = true, true
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--":
			opts.Files = append(opts.Files, args[i+1:]...)

			return opts, nil
		case strings.HasPrefix(arg, "--"):
			consumed, err := f.long(opts, arg[2:], args[i+1:])
			if err != nil {
				return nil, err
			}

			i += consumed
		case len(arg) > 1 && arg[0] == '-':
			consumed, err := f.cluster(opts, arg[1:], args[i+1:])
			if err != nil {
				return nil, err
			}

			i += consumed
		default:
			opts.Files = append(opts.Files, arg)
		}
	}

	return opts, nil
}

// cluster applies a run of short options (the letters after one dash): a
// run of digits is one level (zstd's -19 is nineteen, not one then nine), a
// tool's own letter is its, the rest are shared. A letter that takes a
// value takes the rest of the cluster or the next word; the result is how
// many following words were consumed.
func (f *Format) cluster(opts *Options, letters string, following []string) (int, error) {
	skip := 0

	for i, letter := range letters {
		if skip > 0 {
			skip--

			continue
		}

		if isDigit(letter) {
			digits := letters[i : i+digitRun(letters[i:])]
			skip = len(digits) - 1

			if err := f.setLevelText(opts, digits); err != nil {
				return 0, err
			}

			continue
		}

		extra, isExtra := f.Extra[string(letter)]
		if isExtra && extra.TakesValue {
			return extraWithValue(opts, letter, extra, letters[i+1:], following)
		}

		if err := applyLetter(opts, letter, extra, isExtra); err != nil {
			return 0, err
		}
	}

	return 0, nil
}

// applyLetter applies one short option that takes no value: a tool's own
// letter is its, the rest are shared.
func applyLetter(opts *Options, letter rune, extra Extra, isExtra bool) error {
	if isExtra {
		return extra.apply(opts, "")
	}

	return shortOption(opts, letter)
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// digitRun is the length of the run of digits s starts with.
func digitRun(s string) int {
	n := 0
	for n < len(s) && isDigit(rune(s[n])) {
		n++
	}

	return n
}

// setLevelText applies a level given as digits; a run too long for an int
// is refused the same way as one out of range.
func (f *Format) setLevelText(opts *Options, digits string) error {
	level, err := strconv.Atoi(digits)
	if err != nil {
		return fmt.Errorf(
			"%w: compression level %s is not between %d and %d", errUsage, digits, f.MinLevel, f.MaxLevel,
		)
	}

	return f.setLevel(opts, level)
}

// extraWithValue applies a tool's own short option that takes a value: the
// rest of the cluster when there is one, the next word otherwise.
func extraWithValue(opts *Options, letter rune, extra Extra, rest string, following []string) (int, error) {
	if rest != "" {
		return 0, extra.apply(opts, rest)
	}

	if len(following) == 0 {
		return 0, fmt.Errorf("%w: option requires an argument -- '%c'", errUsage, letter)
	}

	return 1, extra.apply(opts, following[0])
}

func shortOption(opts *Options, letter rune) error {
	switch letter {
	case 'c':
		opts.Stdout = true
	case 'd':
		opts.Decompress = true
	case 'z':
		opts.Decompress = false
	case 'f':
		opts.Force = true
	case 'k':
		opts.Keep = true
	case 'q':
		opts.Quiet = true
	case 'v':
	case 't':
		opts.Test = true
	case 'h':
		opts.Help = true
	case 'V':
		opts.Version = true
	default:
		return fmt.Errorf("%w: invalid option -- '%c'", errUsage, letter)
	}

	return nil
}

func (f *Format) long(opts *Options, spec string, following []string) (int, error) {
	name, value, hasValue := strings.Cut(spec, "=")

	if f.Abbreviations && name != "" {
		resolved, err := f.resolveLong(name)
		if err != nil {
			return 0, err
		}

		name = resolved
	}

	if extra, ok := f.Extra[name]; ok {
		return longExtra(opts, name, extra, value, hasValue, following)
	}

	if hasValue {
		return 0, fmt.Errorf("%w: option '--%s' doesn't allow an argument", errUsage, name)
	}

	switch name {
	case "stdout", "to-stdout":
		opts.Stdout = true
	case "decompress", "uncompress":
		opts.Decompress = true
	case "compress":
		opts.Decompress = false
	case "force":
		opts.Force = true
	case "keep":
		opts.Keep = true
	case "quiet":
		opts.Quiet = true
	case "verbose":
	case "test":
		opts.Test = true
	case "fast":
		return 0, f.setLevel(opts, f.MinLevel)
	case "best":
		return 0, f.setLevel(opts, f.MaxLevel)
	case "help":
		opts.Help = true
	case "version":
		opts.Version = true
	default:
		return 0, fmt.Errorf("%w: unrecognized option '--%s'", errUsage, name)
	}

	return 0, nil
}

// sharedLong are the long options every tool takes, the names long switches on.
func sharedLong() []string {
	return []string{
		"stdout", "to-stdout", "decompress", "uncompress", "compress", "force", "keep",
		"quiet", "verbose", "test", "fast", "best", "help", "version",
	}
}

// resolveLong is the long option the given text names, among the shared
// ones and the tool's own: a whole name, or the one it is a prefix of.
func (f *Format) resolveLong(given string) (string, error) {
	names := sharedLong()

	for name := range f.Extra {
		if utf8.RuneCountInString(name) > 1 {
			names = append(names, name)
		}
	}

	name, diag := longopt.Resolve(given, names)
	if diag != "" {
		return "", fmt.Errorf("%w: %s", errUsage, diag)
	}

	return name, nil
}

// longExtra applies a tool's own long option, with its value after '=' or
// in the next word when it takes one.
func longExtra(
	opts *Options, name string, extra Extra, value string, hasValue bool, following []string,
) (int, error) {
	if !extra.TakesValue {
		if hasValue {
			return 0, fmt.Errorf("%w: option '--%s' doesn't allow an argument", errUsage, name)
		}

		return 0, extra.apply(opts, "")
	}

	if hasValue {
		return 0, extra.apply(opts, value)
	}

	if len(following) == 0 {
		return 0, fmt.Errorf("%w: option '--%s' requires an argument", errUsage, name)
	}

	return 1, extra.apply(opts, following[0])
}

func (f *Format) setLevel(opts *Options, level int) error {
	if level < f.MinLevel || level > f.MaxLevel {
		return fmt.Errorf("%w: compression level %d is not between %d and %d", errUsage, level, f.MinLevel, f.MaxLevel)
	}

	opts.Level = level

	return nil
}

func (e Extra) apply(opts *Options, value string) error {
	if e.Apply == nil {
		return nil
	}

	return e.Apply(opts, value)
}

// runner processes the files of one invocation.
type runner struct {
	format *Format
	opts   *Options
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	status int
}

func (r *runner) warnf(format string, args ...any) {
	if r.status == ExitOK {
		r.status = r.format.Warning
	}

	if !r.opts.Quiet {
		fmt.Fprintf(r.stderr, r.format.Name+": "+format+"\n", args...)
	}
}

func (r *runner) failf(format string, args ...any) {
	r.status = ExitError

	fmt.Fprintf(r.stderr, r.format.Name+": "+format+"\n", args...)
}

// file processes one name: "-" is the standard streams.
func (r *runner) file(name string) {
	if name == "-" {
		out := r.stdout
		if r.opts.Test {
			out = io.Discard
		}

		if err := r.stream(r.stdin, out, Header{}); err != nil {
			r.failf("(stdin): %v", err)
		}

		return
	}

	info, err := os.Stat(name)
	if err != nil {
		r.failf("%s: %v", name, reason(err))

		return
	}

	if !info.Mode().IsRegular() {
		r.warnf("%s: %v -- ignored", name, errNotRegular)

		return
	}

	if r.opts.Test || r.opts.Stdout {
		r.toStream(name, info)

		return
	}

	r.toFile(name, info)
}

// toStream decompresses or compresses name to standard output, or to
// nothing under --test; the file stays.
func (r *runner) toStream(name string, info os.FileInfo) {
	source, err := xos.Open(name)
	if err != nil {
		r.failf("%s: %v", name, reason(err))

		return
	}

	defer func() { _ = source.Close() }() // read-only: a close error carries nothing

	out := r.stdout
	if r.opts.Test {
		out = io.Discard
	}

	if err := r.stream(source, out, r.header(name, info)); err != nil {
		r.failf("%s: %v", name, err)
	}
}

// toFile compresses or decompresses name into its counterpart beside it,
// then removes name unless --keep.
func (r *runner) toFile(name string, info os.FileInfo) {
	target, ok := r.target(name)
	if !ok {
		return
	}

	source, err := xos.Open(name)
	if err != nil {
		r.failf("%s: %v", name, reason(err))

		return
	}

	defer func() { _ = source.Close() }() // read-only: a close error carries nothing

	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if r.opts.Force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	out, err := xos.OpenFile(target, flags, info.Mode().Perm())
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			r.warnf("%s already exists; not overwritten", target)
		} else {
			r.failf("%s: %v", target, reason(err))
		}

		return
	}

	if err := r.stream(source, out, r.header(name, info)); err != nil {
		_ = out.Close()
		_ = os.Remove(target)

		r.failf("%s: %v", name, err)

		return
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(target)

		r.failf("%s: %v", target, reason(err))

		return
	}

	// The counterpart takes the original's mode exactly, as the tools do (the
	// create above went through the umask), and its times; a failure on
	// either is not worth losing the result over.
	_ = os.Chmod(target, info.Mode().Perm())
	_ = os.Chtimes(target, info.ModTime(), info.ModTime())

	if r.opts.Keep {
		return
	}

	// Closed before the remove: Windows refuses to remove an open file.
	_ = source.Close()

	if err := os.Remove(name); err != nil {
		r.failf("%s: %v", name, reason(err))
	}
}

// target is the counterpart's name: with the suffix on compression, without
// it on decompression, where a name with no known suffix is skipped.
func (r *runner) target(name string) (string, bool) {
	if !r.opts.Decompress {
		if _, known := r.suffix(name); known && !r.opts.Force {
			r.warnf("%s already has the %s suffix -- unchanged", name, r.format.Suffix)

			return "", false
		}

		return name + r.format.Suffix, true
	}

	suffix, known := r.suffix(name)
	if !known {
		r.warnf("%s: unknown suffix -- ignored", name)

		return "", false
	}

	return strings.TrimSuffix(name, suffix) + r.format.Suffixes[suffix], true
}

// suffix is the known compressed suffix name ends with, the longest first.
func (r *runner) suffix(name string) (string, bool) {
	lower := strings.ToLower(name)

	best := ""

	for suffix := range r.format.Suffixes {
		if strings.HasSuffix(lower, suffix) && len(suffix) > len(best) {
			best = suffix
		}
	}

	if best == "" {
		return "", false
	}

	return name[len(name)-len(best):], true
}

func (r *runner) header(name string, info os.FileInfo) Header {
	if r.opts.NoName {
		return Header{}
	}

	return Header{Name: filepath.Base(name), ModTime: info.ModTime().Unix()}
}

// stream runs one direction between the two streams.
func (r *runner) stream(source io.Reader, out io.Writer, header Header) error {
	if r.opts.Decompress {
		decoder, err := r.format.Decompress(source)
		if err != nil {
			return err
		}

		if _, err := io.Copy(out, decoder); err != nil {
			_ = decoder.Close()

			return err //nolint:wrapcheck // the decoder's words are the diagnostic
		}

		return decoder.Close() //nolint:wrapcheck // the decoder's words are the diagnostic
	}

	if isTerminal(out) && !r.opts.Force {
		return errIsTerminal
	}

	encoder, err := r.format.Compress(out, r.opts.Level, header)
	if err != nil {
		return err
	}

	if _, err := io.Copy(encoder, source); err != nil {
		_ = encoder.Close()

		return err //nolint:wrapcheck // the encoder's words are the diagnostic
	}

	return encoder.Close() //nolint:wrapcheck // the encoder's words are the diagnostic
}

// isTerminal: the writer is a character device, a terminal in practice.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// reason is the OS's reason alone: the path is already in the message.
func reason(err error) error {
	if pathErr, ok := errors.AsType[*os.PathError](err); ok {
		return pathErr.Err
	}

	return err
}

// ParseThreads reads a --threads value: a non-negative count, 0 for all.
func ParseThreads(value string) error {
	if n, err := strconv.Atoi(value); err != nil || n < 0 {
		return fmt.Errorf("%w: invalid thread count '%s'", errUsage, value)
	}

	return nil
}

// Usagef is a usage error for a tool's own option, reported as the shared
// ones are.
func Usagef(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, args...))
}

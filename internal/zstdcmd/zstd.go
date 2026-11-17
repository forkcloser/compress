// Package zstdcmd is the zstd command: zstd's file-by-file behavior for the
// common options, over klauspost/compress. unzstd decompresses, zstdcat
// decompresses to standard output, zstdmt is zstd under another name.
package zstdcmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/klauspost/compress/zstd"

	"github.com/forkcloser/compress/internal/filecodec"
)

const helpText = `Usage: zstd [OPTIONS...] [INPUT... | -]
Compress or decompress the INPUT file(s); reads from STDIN if INPUT is - or not provided.

  -z, --compress      force compression
  -d, --decompress    perform decompression
  -t, --test          test compressed file integrity
  -k, --keep          preserve INPUT file(s) [default]
      --rm            remove INPUT file(s) after successful (de)compression
  -f, --force         allow overwriting existing files and writing to a console
  -c, --stdout        write to standard output (implies --keep)
  -1 ... -19          compression level; default is 3
  -T#, --threads=#    accepted for compatibility
      --long[=#]      decompression: let a frame declare a window of 2^# bytes
                      (27 without a number; 128 MiB without the option)
  -q, --quiet         suppress warnings
  -h, --help          display this help and exit
  -V, --version       display the version number and exit
`

// zstd's limit on the window a frame may declare, and the --long that
// moves it: two to the power 10 to 31, 27 when it names none.
const (
	defaultWindow    = 128 << 20
	defaultWindowLog = 27
	minWindowLog     = 10
	maxWindowLog     = 31
)

// windowed words a frame refused for its window with what allows it: the
// decoder's own words name no option.
type windowed struct {
	io.ReadCloser
}

func (w *windowed) Read(p []byte) (int, error) {
	n, err := w.ReadCloser.Read(p)
	if errors.Is(err, zstd.ErrWindowSizeExceeded) {
		return n, fmt.Errorf("%w (--long=N allows 2^N bytes)", err)
	}

	return n, err //nolint:wrapcheck // the decoder's words are the diagnostic
}

// zstd's levels: 1 through 19 without --ultra, 3 by default.
const (
	minLevel     = 1
	maxLevel     = 19
	defaultLevel = 3
)

var errEmpty = errors.New("unexpected end of file")

// Format is the zstd tool.
func Format() *filecodec.Format {
	ignored := filecodec.Extra{}

	// The window a frame may declare: the decoder takes that much memory
	// before it decodes a byte.
	window := uint64(defaultWindow)

	// The one format this zstd writes: a value that would change the stream
	// is refused rather than silently ignored.
	format := filecodec.Extra{TakesValue: true, Apply: func(_ *filecodec.Options, v string) error {
		if v != "zstd" {
			return filecodec.Usagef("%s: Unsupported file format type (only zstd is written)", v)
		}

		return nil
	}}
	threads := filecodec.Extra{
		TakesValue: true,
		Apply:      func(_ *filecodec.Options, v string) error { return filecodec.ParseThreads(v) },
	}

	return &filecodec.Format{
		Name:         "zstd",
		Suffix:       ".zst",
		Suffixes:     map[string]string{".zst": "", ".tzst": ".tar"},
		MinLevel:     minLevel,
		MaxLevel:     maxLevel,
		DefaultLevel: defaultLevel,
		// zstd keeps its inputs unless --rm, and has no warning status: a
		// skipped file is an error.
		Keep:    true,
		Warning: filecodec.ExitError,
		Links:   filecodec.LinksSymbolic,
		Lacks:   []string{"best", "to-stdout"},
		Compress: func(w io.Writer, level int, _ filecodec.Header) (io.WriteCloser, error) {
			writer, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
			if err != nil {
				return nil, fmt.Errorf("zstd encoder: %w", err)
			}

			return writer, nil
		},
		Decompress: func(r io.Reader) (io.ReadCloser, error) {
			// The decoder takes no frame at all for a whole stream of
			// nothing.
			source := bufio.NewReader(r)
			if _, err := source.Peek(1); errors.Is(err, io.EOF) {
				return nil, errEmpty
			}

			reader, err := zstd.NewReader(source, zstd.WithDecoderMaxWindow(window))
			if err != nil {
				return nil, fmt.Errorf("zstd decoder: %w", err)
			}

			return &windowed{reader.IOReadCloser()}, nil
		},
		Extra: map[string]filecodec.Extra{
			"rm":          {Apply: func(o *filecodec.Options, _ string) error { o.Keep = false; return nil }},
			"T":           threads,
			"threads":     threads,
			"ultra":       ignored,
			"long":        long(&window),
			"no-progress": ignored,
			"format":      format,
		},
		Help: helpText,
	}
}

// long is --long: it sets the window a frame may declare, the first of
// several does, to two to the power it names or to 27.
func long(window *uint64) filecodec.Extra {
	set := false

	return filecodec.Extra{MayTakeValue: true, Apply: func(_ *filecodec.Options, value string) error {
		log := defaultWindowLog

		if value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < minWindowLog || parsed > maxWindowLog {
				return filecodec.Usagef("--long=%s: the window is two to a power of %d to %d", value, minWindowLog,
					maxWindowLog)
			}

			log = parsed
		}

		if !set {
			*window, set = 1<<log, true
		}

		return nil
	}}
}

// Run is the zstd command; name is how it was invoked.
func Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	return Format().Run(name, args, stdin, stdout, stderr, version)
}

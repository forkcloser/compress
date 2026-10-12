// Package zstdcmd is the zstd command: zstd's file-by-file behavior for the
// common options, over klauspost/compress. unzstd decompresses, zstdcat
// decompresses to standard output, zstdmt is zstd under another name.
package zstdcmd

import (
	"fmt"
	"io"

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
  -q, --quiet         suppress warnings
  -h, --help          display this help and exit
  -V, --version       display the version number and exit
`

// zstd's levels: 1 through 19 without --ultra, 3 by default.
const (
	minLevel     = 1
	maxLevel     = 19
	defaultLevel = 3
)

// Format is the zstd tool.
func Format() *filecodec.Format {
	ignored := filecodec.Extra{}
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
		Compress: func(w io.Writer, level int, _ filecodec.Header) (io.WriteCloser, error) {
			writer, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
			if err != nil {
				return nil, fmt.Errorf("zstd encoder: %w", err)
			}

			return writer, nil
		},
		Decompress: func(r io.Reader) (io.ReadCloser, error) {
			reader, err := zstd.NewReader(r)
			if err != nil {
				return nil, fmt.Errorf("zstd decoder: %w", err)
			}

			return reader.IOReadCloser(), nil
		},
		Extra: map[string]filecodec.Extra{
			"rm":          {Apply: func(o *filecodec.Options, _ string) error { o.Keep = false; return nil }},
			"T":           threads,
			"threads":     threads,
			"ultra":       ignored,
			"no-progress": ignored,
			"format":      format,
		},
		Help: helpText,
	}
}

// Run is the zstd command; name is how it was invoked.
func Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	return Format().Run(name, args, stdin, stdout, stderr, version)
}

// Package xzcmd is the xz command: xz's file-by-file behavior for the
// options the rig uses, over forkcloser/xz. unxz decompresses, xzcat
// decompresses to standard output.
package xzcmd

import (
	"fmt"
	"io"

	uxz "github.com/forkcloser/xz"

	"github.com/forkcloser/compress/internal/constants"
	"github.com/forkcloser/compress/internal/filecodec"
)

const helpText = `Usage: xz [OPTION]... [FILE]...
Compress or decompress FILEs in the .xz format.

  -z, --compress      force compression
  -d, --decompress    force decompression
  -t, --test          test compressed file integrity
  -k, --keep          keep (don't delete) input files
  -f, --force         force overwrite of output file and (de)compress links
  -c, --stdout        write to standard output and don't delete input files
  -0 ... -9           compression preset; default is 6
  -T, --threads=NUM   accepted for compatibility; decoding is parallel already
  -q, --quiet         suppress warnings
  -h, --help          display this help and exit
  -V, --version       display the version number and exit

With no FILE, or when FILE is -, read standard input.
`

// dictCaps are xz's presets as dictionary sizes: 0 through 9.
func dictCaps() []int {
	return []int{
		256 * constants.KiB, constants.MiB, 2 * constants.MiB, 4 * constants.MiB, 4 * constants.MiB,
		8 * constants.MiB, 8 * constants.MiB, 16 * constants.MiB, 32 * constants.MiB, 64 * constants.MiB,
	}
}

// Format is the xz tool.
func Format() *filecodec.Format {
	ignored := filecodec.Extra{}
	withValue := filecodec.Extra{TakesValue: true}
	// The one format and the one check this xz writes: a value that would
	// change the stream is refused rather than silently ignored.
	format := filecodec.Extra{TakesValue: true, Apply: func(_ *filecodec.Options, v string) error {
		if v != "xz" && v != "auto" {
			return filecodec.Usagef("%s: Unsupported file format type (only xz is written)", v)
		}

		return nil
	}}
	check := filecodec.Extra{TakesValue: true, Apply: func(_ *filecodec.Options, v string) error {
		if v != "crc64" {
			return filecodec.Usagef("%s: Unsupported integrity check type (only crc64 is written)", v)
		}

		return nil
	}}

	return &filecodec.Format{
		Name:          "xz",
		Suffix:        ".xz",
		Suffixes:      map[string]string{".xz": "", ".txz": ".tar", ".lzma": "", ".tlz": ".tar"},
		MinLevel:      0,
		MaxLevel:      9,
		DefaultLevel:  6,
		Warning:       filecodec.ExitWarning,
		Abbreviations: true,
		Compress: func(w io.Writer, level int, _ filecodec.Header) (io.WriteCloser, error) {
			writer, err := uxz.WriterConfig{DictCap: dictCaps()[level]}.NewWriter(w)
			if err != nil {
				return nil, fmt.Errorf("xz encoder: %w", err)
			}

			return writer, nil
		},
		Decompress: func(r io.Reader) (io.ReadCloser, error) {
			reader, err := uxz.NewReader(r)
			if err != nil {
				return nil, fmt.Errorf("File format not recognized: %w", err)
			}

			return io.NopCloser(reader), nil
		},
		Extra: map[string]filecodec.Extra{
			"T": {
				TakesValue: true,
				Apply:      func(_ *filecodec.Options, v string) error { return filecodec.ParseThreads(v) },
			},
			"threads": {
				TakesValue: true,
				Apply:      func(_ *filecodec.Options, v string) error { return filecodec.ParseThreads(v) },
			},
			"e":                   ignored,
			"extreme":             ignored,
			"F":                   format,
			"format":              format,
			"C":                   check,
			"check":               check,
			"memlimit":            withValue,
			"memlimit-compress":   withValue,
			"memlimit-decompress": withValue,
			"M":                   withValue,
		},
		Help: helpText,
	}
}

// Run is the xz command; name is how it was invoked.
func Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	return Format().Run(name, args, stdin, stdout, stderr, version)
}

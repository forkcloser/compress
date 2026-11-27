// Package xzcmd is the xz command: xz's file-by-file behavior for the
// options the rig uses, over forkcloser/go-xz. unxz decompresses, xzcat
// decompresses to standard output.
package xzcmd

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	uxz "github.com/forkcloser/go-xz/v2"

	"github.com/forkcloser/go-compress/internal/constants"
	"github.com/forkcloser/go-compress/internal/filecodec"
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
  -M, --memlimit=LIMIT, --memlimit-compress=LIMIT, --memlimit-decompress=LIMIT
                      accepted for compatibility; no limit is enforced
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

// parseMemlimit refuses a memory limit xz refuses: it takes a count of
// bytes, or a percentage of the memory there is, from 1% to 100%.
func parseMemlimit(value string) error {
	if digits, percent := strings.CutSuffix(value, "%"); percent && digits != "" {
		return parseCount(digits, 1, maxPercent)
	}

	return parseCount(value, 0, math.MaxUint64)
}

const (
	maxPercent = 100
	// kibi is the bits a multiplier of 1024 shifts a count by: M shifts by
	// two of them, G by three.
	kibi = 10
)

// parseCount reads a count as xz does and refuses one outside lowest to
// highest: "max", or decimal digits and a multiplier by 1024 (k), its
// square (M) or its cube (G), in either case and spelled alone or with
// "i", "iB" or "B" after it.
func parseCount(value string, lowest, highest uint64) error {
	value = strings.TrimLeft(value, " \t")
	if value == "max" {
		return nil
	}

	digits := value[:len(value)-len(strings.TrimLeft(value, "0123456789"))]
	if digits == "" {
		return filecodec.Usagef("%s: Value is not a non-negative decimal integer", value)
	}

	shift := 0

	if suffix := value[len(digits):]; suffix != "" {
		shift = kibi * (strings.Index("kmg", strings.ToLower(suffix[:1])) + 1)

		if !slices.Contains([]string{"", "i", "iB", "B"}, suffix[1:]) || shift == 0 {
			return filecodec.Usagef("%s: Invalid multiplier suffix", suffix)
		}
	}

	// A count the multiplier carries past 64 bits is past every range.
	count, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || count > math.MaxUint64>>shift || count<<shift < lowest || count<<shift > highest {
		return filecodec.Usagef("Value of a memory limit must be in the range [%d, %d]", lowest, highest)
	}

	return nil
}

// Format is the xz tool.
func Format() *filecodec.Format {
	ignored := filecodec.Extra{}
	// A limit is read as xz reads one, so that what xz refuses is refused,
	// and then not enforced: there is no measure here of what a stream
	// takes.
	memlimit := filecodec.Extra{TakesValue: true, Apply: func(_ *filecodec.Options, v string) error {
		return parseMemlimit(v)
	}}
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
		Suffixes:      map[string]string{".xz": "", ".txz": ".tar"},
		MinLevel:      0,
		MaxLevel:      9,
		DefaultLevel:  6,
		Warning:       filecodec.ExitWarning,
		Links:         filecodec.LinksWhereRemoved,
		ExistsIsError: true,
		SkipSuffixed:  true,
		Abbreviations: true,
		Compress: func(w io.Writer, level int, _ filecodec.Header) (io.WriteCloser, error) {
			writer, err := uxz.NewWriter(w, uxz.WithDictSize(dictCaps()[level]))
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
			"memlimit":            memlimit,
			"memlimit-compress":   memlimit,
			"memlimit-decompress": memlimit,
			"M":                   memlimit,
		},
		Unsupported: []string{
			"arm", "arm64", "armthumb", "block-list", "block-size", "delta", "files", "files0", "filters",
			"filters-help", "filters1", "filters2", "filters3", "filters4", "filters5", "filters6", "filters7",
			"filters8", "filters9", "flush-timeout", "ia64", "ignore-check", "info-memory", "list", "long-help",
			"lzma1", "lzma2", "memlimit-mt-decompress", "memory", "no-adjust", "no-sparse", "no-sync", "no-warn",
			"powerpc", "riscv", "robot", "single-stream", "sparc", "suffix", "x86",
		},
		Help: helpText,
	}
}

// Run is the xz command; name is how it was invoked.
func Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	return Format().Run(name, args, stdin, stdout, stderr, version)
}

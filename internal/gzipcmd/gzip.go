// Package gzipcmd is the gzip command: gzip's file-by-file behavior for the
// options the rig uses, over klauspost/compress. gunzip decompresses, zcat
// decompresses to standard output.
package gzipcmd

import (
	"fmt"
	"io"
	"time"

	kgzip "github.com/klauspost/compress/gzip"

	"github.com/forkcloser/compress/internal/filecodec"
)

const helpText = `Usage: gzip [OPTION]... [FILE]...
Compress or uncompress FILEs (by default, compress FILES in-place).

  -c, --stdout      write on standard output, keep original files unchanged
  -d, --decompress  decompress
  -f, --force       force overwrite of output file
  -k, --keep        keep (don't delete) input files
  -n, --no-name     do not save or restore the original name and timestamp
  -N, --name        save or restore the original name and timestamp
  -q, --quiet       suppress all warnings
  -t, --test        test compressed file integrity
  -1, --fast        compress faster
  -9, --best        compress better
  -h, --help        give this help
  -V, --version     display version number

With no FILE, or when FILE is -, read standard input.
`

// Format is the gzip tool.
func Format() *filecodec.Format {
	return &filecodec.Format{
		Name:         "gzip",
		Suffix:       ".gz",
		Suffixes:     map[string]string{".gz": "", ".tgz": ".tar", ".taz": ".tar", "-gz": "", ".z": "", "-z": ""},
		MinLevel:     1,
		MaxLevel:     9,
		DefaultLevel: 6,
		Compress: func(w io.Writer, level int, header filecodec.Header) (io.WriteCloser, error) {
			writer, err := kgzip.NewWriterLevel(w, level)
			if err != nil {
				return nil, fmt.Errorf("gzip encoder: %w", err)
			}

			// The epoch, not the zero time: the header field is seconds since
			// the epoch, and the writer encodes whatever time it is given.
			writer.Name = header.Name
			writer.ModTime = time.Unix(header.ModTime, 0)

			return writer, nil
		},
		Decompress: func(r io.Reader) (io.ReadCloser, error) {
			reader, err := kgzip.NewReader(r)
			if err != nil {
				return nil, fmt.Errorf("not in gzip format: %w", err)
			}

			return reader, nil
		},
		Extra: map[string]filecodec.Extra{
			"n":       {Apply: func(o *filecodec.Options, _ string) error { o.NoName = true; return nil }},
			"no-name": {Apply: func(o *filecodec.Options, _ string) error { o.NoName = true; return nil }},
			"N":       {Apply: func(o *filecodec.Options, _ string) error { o.NoName = false; return nil }},
			"name":    {Apply: func(o *filecodec.Options, _ string) error { o.NoName = false; return nil }},
		},
		Help: helpText,
	}
}

// Run is the gzip command; name is how it was invoked.
func Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	return Format().Run(name, args, stdin, stdout, stderr, version)
}

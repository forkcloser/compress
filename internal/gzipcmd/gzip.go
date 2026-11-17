// Package gzipcmd is the gzip command: gzip's file-by-file behavior for the
// options the rig uses, over klauspost/compress. gunzip decompresses, zcat
// decompresses to standard output.
package gzipcmd

import (
	"bufio"
	"errors"
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
  -f, --force       force overwrite of output file and compress links
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

// magic is the byte every member begins with.
const magic = 0x1f

// members is a gzip file read one member at a time: what follows the last
// and does not begin another is trailing garbage, where a reader of the
// whole file takes it for a member that fails.
type members struct {
	source *bufio.Reader
	member *kgzip.Reader
}

func (m *members) Read(p []byte) (int, error) {
	for {
		n, err := m.member.Read(p)
		if n > 0 {
			// The member keeps an error it had with these bytes for the
			// next read.
			return n, nil
		}

		if !errors.Is(err, io.EOF) {
			return 0, err //nolint:wrapcheck // the decoder's words are the diagnostic
		}

		if err := m.next(); err != nil {
			return 0, err
		}
	}
}

func (m *members) Close() error {
	return m.member.Close() //nolint:wrapcheck // the decoder's words are the diagnostic
}

// next starts the member the input goes on with. It answers io.EOF where
// the input ends, after zeros or not: gzip takes zeros after a member for
// padding, of a tape's block say, and says nothing of them. It answers
// filecodec.ErrTrailingGarbage where the input goes on with a byte no
// member begins with, or with anything at all after zeros: padding ends
// the members.
func (m *members) next() error {
	head, err := m.source.Peek(1)
	if err != nil {
		return err //nolint:wrapcheck // io.EOF is the reader's contract
	}

	if head[0] == 0 {
		for {
			padding, err := m.source.ReadByte()
			if err != nil {
				return err //nolint:wrapcheck // io.EOF is the reader's contract
			}

			if padding != 0 {
				return filecodec.ErrTrailingGarbage
			}
		}
	}

	if head[0] != magic {
		return filecodec.ErrTrailingGarbage
	}

	if err := m.member.Reset(m.source); err != nil {
		return err //nolint:wrapcheck // the decoder's words are the diagnostic
	}

	m.member.Multistream(false)

	return nil
}

// Format is the gzip tool.
func Format() *filecodec.Format {
	// The last of -n and -N decides both directions: what is saved on
	// compression, what is restored on decompression.
	noName := filecodec.Extra{Apply: func(o *filecodec.Options, _ string) error {
		o.NoName, o.Name = true, false

		return nil
	}}
	name := filecodec.Extra{Apply: func(o *filecodec.Options, _ string) error {
		o.NoName, o.Name = false, true

		return nil
	}}

	return &filecodec.Format{
		Name:   "gzip",
		Suffix: ".gz",
		Suffixes: map[string]string{
			".gz": "", ".tgz": ".tar", ".taz": ".tar", "-gz": "", ".z": "", "-z": "", "_z": "",
		},
		FoldSuffix:     true,
		LinkFails:      true,
		SuffixedIsFine: true,
		MinLevel:       1,
		MaxLevel:       9,
		DefaultLevel:   6,
		Warning:        filecodec.ExitWarning,
		Links:          filecodec.LinksToFile,
		SkipSuffixed:   true,
		Lacks:          []string{"z", "compress"},
		Abbreviations:  true,
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
			// One buffer under every member: a member's own would take the
			// bytes after it along.
			source := bufio.NewReader(r)

			member, err := kgzip.NewReader(source)
			if err != nil {
				return nil, fmt.Errorf("not in gzip format: %w", err)
			}

			member.Multistream(false)

			return &members{source: source, member: member}, nil
		},
		Original: func(r io.Reader) (filecodec.Header, error) {
			member, err := kgzip.NewReader(r)
			if err != nil {
				return filecodec.Header{}, fmt.Errorf("not in gzip format: %w", err)
			}

			defer func() { _ = member.Close() }() // read-only: a close error carries nothing

			// The zero time is the header's "no time", not an instant.
			original := filecodec.Header{Name: member.Name}
			if !member.ModTime.IsZero() {
				original.ModTime = member.ModTime.Unix()
			}

			return original, nil
		},
		Extra: map[string]filecodec.Extra{
			"n": noName, "no-name": noName,
			"N": name, "name": name,
		},
		Unsupported: []string{
			"ascii", "bits", "license", "list", "lzw", "recursive", "rsyncable", "silent", "suffix", "synchronous",
		},
		Help: helpText,
	}
}

// Run is the gzip command; name is how it was invoked.
func Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	return Format().Run(name, args, stdin, stdout, stderr, version)
}

package tarcmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	kgzip "github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	plz4 "github.com/pierrec/lz4/v4"

	uxz "github.com/forkcloser/xz"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/bzip2"
	"github.com/forkcloser/compress/gzip"
	"github.com/forkcloser/compress/lz4"
	"github.com/forkcloser/compress/xz"
)

var (
	errNoBzip2Writer      = errors.New("bzip2 compression is not supported")
	errUnknownCompression = errors.New("unknown compression")
)

// readBufferSize amortizes the archive's reads for every decoder.
const readBufferSize = 256 * compress.KiB

// magics are the leading bytes of each compressed stream, longest first so a
// prefix of another never wins.
func magics() []struct {
	compression compression
	magic       []byte
} {
	return []struct {
		compression compression
		magic       []byte
	}{
		{compressXz, []byte{0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00}},
		{compressZstd, []byte{0x28, 0xB5, 0x2F, 0xFD}},
		{compressLz4, []byte{0x04, 0x22, 0x4D, 0x18}},
		{compressBzip2, []byte{0x42, 0x5A, 0x68}},
		{compressGzip, []byte{0x1F, 0x8B}},
	}
}

const magicPeek = 6

// bySuffix is -a's rule: the compression an archive name implies, none for a
// name without a known suffix.
func bySuffix(name string) compression {
	lower := strings.ToLower(name)

	for _, candidate := range []struct {
		suffix      string
		compression compression
	}{
		{".tar.gz", compressGzip},
		{".tgz", compressGzip},
		{".taz", compressGzip},
		{".gz", compressGzip},
		{".tar.xz", compressXz},
		{".txz", compressXz},
		{".xz", compressXz},
		{".tar.bz2", compressBzip2},
		{".tbz2", compressBzip2},
		{".tbz", compressBzip2},
		{".tz2", compressBzip2},
		{".bz2", compressBzip2},
		{".tar.zst", compressZstd},
		{".tzst", compressZstd},
		{".zst", compressZstd},
		{".tar.lz4", compressLz4},
		{".lz4", compressLz4},
	} {
		if strings.HasSuffix(lower, candidate.suffix) {
			return candidate.compression
		}
	}

	return compressNone
}

// openReader wraps the archive's reader in the decompressor the options
// name, or the one the stream's magic names when they name none; a stream
// with no known magic is a plain archive.
func openReader(wrap compression, reader io.Reader) (io.ReadCloser, error) {
	buffered := bufio.NewReaderSize(reader, readBufferSize)

	if wrap == compressAuto {
		wrap = compressNone

		header, _ := buffered.Peek(magicPeek) // a short stream is a plain archive's problem, reported as such
		for _, m := range magics() {
			if bytes.HasPrefix(header, m.magic) {
				wrap = m.compression

				break
			}
		}
	}

	switch wrap {
	case compressNone, compressAuto:
		return io.NopCloser(buffered), nil
	case compressGzip:
		return gzip.New(buffered)
	case compressXz:
		return xz.New(buffered)
	case compressBzip2:
		return bzip2.New(buffered)
	case compressZstd:
		return compress.Decompress(buffered)
	case compressLz4:
		return lz4.New(buffered)
	}

	return nil, fmt.Errorf("%w: %d", errUnknownCompression, wrap)
}

// nopWriteCloser is a plain archive's writer: the file is closed by the caller.
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

// openWriter wraps the archive's writer in the compressor the options name.
func openWriter(wrap compression, writer io.Writer) (io.WriteCloser, error) {
	switch wrap {
	case compressNone, compressAuto:
		return nopWriteCloser{writer}, nil
	case compressGzip:
		return kgzip.NewWriter(writer), nil
	case compressXz:
		encoder, err := uxz.NewWriter(writer)
		if err != nil {
			return nil, fmt.Errorf("xz encoder: %w", err)
		}

		return encoder, nil
	case compressZstd:
		encoder, err := zstd.NewWriter(writer)
		if err != nil {
			return nil, fmt.Errorf("zstd encoder: %w", err)
		}

		return encoder, nil
	case compressLz4:
		return plz4.NewWriter(writer), nil
	case compressBzip2:
		return nil, errNoBzip2Writer
	}

	return nil, fmt.Errorf("%w: %d", errUnknownCompression, wrap)
}

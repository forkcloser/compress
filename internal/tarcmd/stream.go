package tarcmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/forkcloser/compress/bzip2"
	"github.com/forkcloser/compress/gzip"
	"github.com/forkcloser/compress/internal/constants"
	"github.com/forkcloser/compress/lz4"
	"github.com/forkcloser/compress/xz"
	"github.com/forkcloser/compress/zstd"
)

var (
	errNoBzip2Writer      = errors.New("bzip2 compression is not supported")
	errUnknownCompression = errors.New("unknown compression")
	errChild              = errors.New("Child returned status")
)

// knownProgram is the codec an --use-compress-program names, by the
// program's base name: the compressors, their decompressing names and
// their parallel builds write and read the same streams as the codecs here.
func knownProgram(program string) (compression, bool) {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(program)), ".exe")

	switch name {
	case "zstd", "zstdmt", "unzstd", "pzstd":
		return compressZstd, true
	case "gzip", "gunzip", "pigz", "unpigz":
		return compressGzip, true
	case "xz", "unxz", "pxz", "pixz":
		return compressXz, true
	case "bzip2", "bunzip2", "lbzip2", "pbzip2":
		return compressBzip2, true
	case "lz4", "unlz4":
		return compressLz4, true
	}

	return compressNone, false
}

// programWriter filters what is written through an external compressor
// into the archive; Close ends its input and waits for it.
type programWriter struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	waited bool
}

// Write reports a program that stopped reading by its exit status, as GNU
// tar does, rather than by the broken pipe it left.
func (p *programWriter) Write(data []byte) (int, error) {
	n, err := p.input.Write(data)
	if err != nil {
		_ = p.input.Close() // the program is gone: its status is the error

		p.waited = true

		if status := waitProgram(p.cmd); status != nil {
			return n, status
		}

		return n, fmt.Errorf("%s: %w", p.cmd.Path, err)
	}

	return n, nil
}

func (p *programWriter) Close() error {
	if p.waited {
		return nil
	}

	if err := p.input.Close(); err != nil {
		return fmt.Errorf("%s: %w", p.cmd.Path, err)
	}

	p.waited = true

	return waitProgram(p.cmd)
}

// programReader reads the archive through an external decompressor; Close
// drains it and waits for it.
type programReader struct {
	cmd    *exec.Cmd
	output io.ReadCloser
}

func (p *programReader) Read(data []byte) (int, error) {
	return p.output.Read(data) //nolint:wrapcheck // io.EOF must reach the reader unwrapped
}

func (p *programReader) Close() error {
	_, _ = io.Copy(io.Discard, p.output) // the rest of the stream: what is left unread is the archive's trailer

	return waitProgram(p.cmd)
}

// waitProgram is GNU tar's report of a filter that failed.
func waitProgram(cmd *exec.Cmd) error {
	err := cmd.Wait()

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return fmt.Errorf("%s: %w %d", cmd.Path, errChild, exit.ExitCode())
	}

	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Path, err)
	}

	return nil
}

// command is the --use-compress-program as an exec.Cmd: the program and its
// arguments split on blanks, as GNU tar does, with -d appended to read.
func command(program string, decompress bool) *exec.Cmd {
	fields := strings.Fields(program)
	if decompress {
		fields = append(fields, "-d")
	}

	// The filter lives exactly as long as the command, which has no
	// cancellation to give it.
	// #nosec G204 -- the program is the command's own --use-compress-program
	cmd := exec.CommandContext(context.Background(), fields[0], fields[1:]...)
	cmd.Stderr = os.Stderr

	return cmd
}

func openProgramReader(program string, reader io.Reader) (io.ReadCloser, error) {
	cmd := command(program, true)
	cmd.Stdin = reader

	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", program, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: Cannot run: %w", program, errReason(err))
	}

	return &programReader{cmd: cmd, output: output}, nil
}

func openProgramWriter(program string, writer io.Writer) (io.WriteCloser, error) {
	cmd := command(program, false)
	cmd.Stdout = writer

	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", program, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: Cannot run: %w", program, errReason(err))
	}

	return &programWriter{cmd: cmd, input: input}, nil
}

// readBufferSize amortizes the archive's reads for every decoder.
const readBufferSize = 256 * constants.KiB

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
func openReader(wrap compression, program string, reader io.Reader) (io.ReadCloser, error) {
	if wrap == compressProgram {
		return openProgramReader(program, reader)
	}

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
		return gzip.NewReader(buffered)
	case compressXz:
		return xz.NewReader(buffered)
	case compressBzip2:
		return bzip2.NewReader(buffered)
	case compressZstd:
		return zstd.NewReader(buffered)
	case compressLz4:
		return lz4.NewReader(buffered)
	case compressProgram:
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
func openWriter(wrap compression, program string, writer io.Writer) (io.WriteCloser, error) {
	switch wrap {
	case compressProgram:
		return openProgramWriter(program, writer)
	case compressNone, compressAuto:
		return nopWriteCloser{writer}, nil
	case compressGzip:
		return gzip.NewWriter(writer)
	case compressXz:
		return xz.NewWriter(writer)
	case compressZstd:
		return zstd.NewWriter(writer)
	case compressLz4:
		return lz4.NewWriter(writer)
	case compressBzip2:
		return nil, errNoBzip2Writer
	}

	return nil, fmt.Errorf("%w: %d", errUnknownCompression, wrap)
}

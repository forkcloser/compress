package unzipcmd_test

// What the contract tests share: the Info-ZIP unzip to compare with, where
// there is one, and an archive written header by header.

import (
	"archive/zip"
	"bytes"
	"errors"
	"hash/crc32"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/reference"
)

// theirs runs the unzip on PATH that is not this module's own, or skips the
// test. Its standard input is empty: a question it would ask gets no answer.
func theirs(t *testing.T, args ...string) (status int, stdout, stderr string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip(
			"the lines are compared with Info-ZIP's unix unzip; what a Windows port prints for a Windows path is not known",
		)
	}

	tool := reference.Find(t, "unzip", "-v")

	var out, diag bytes.Buffer

	cmd := exec.CommandContext(t.Context(), tool, args...)
	cmd.Stdout, cmd.Stderr = &out, &diag

	if err := cmd.Run(); err != nil {
		exit, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("%s %s: %v", tool, strings.Join(args, " "), err)
		}

		status = exit.ExitCode()
	}

	return status, out.String(), diag.String()
}

// raw is an entry written as its header says, the data untouched: a method
// or a flag archive/zip would not write itself.
type raw struct {
	header zip.FileHeader
	data   string
}

// buildRaw writes an archive of the entries to path.
func buildRaw(t *testing.T, path string, entries ...raw) string {
	t.Helper()

	var buf bytes.Buffer

	writer := zip.NewWriter(&buf)

	for _, entry := range entries {
		header := entry.header
		header.CompressedSize64 = uint64(len(entry.data))

		out, err := writer.CreateRaw(&header)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := out.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

// stored is the header of an entry whose data is its content as it is.
func stored(name, content string) zip.FileHeader {
	header := zip.FileHeader{Name: name, Method: zip.Store, UncompressedSize64: uint64(len(content))}
	header.CRC32 = crc32.ChecksumIEEE([]byte(content))
	header.Modified = stamp()
	header.SetMode(0o644)

	return header
}

// trimLines drops the spaces a line ends with.
func trimLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}

	return strings.Join(lines, "\n")
}

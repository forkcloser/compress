package xzcmd_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/filecodec"
)

// TestContractLzmaIsNotThisXzsSuffix: this xz reads the .xz format only,
// so .lzma and .tlz are not suffixes it decompresses by: such a file has
// an unknown suffix, status 2, and is left alone, whatever it holds.
// Compressing one adds .xz, as for any file.
func TestContractLzmaIsNotThisXzsSuffix(t *testing.T) {
	t.Parallel()

	stream := mustRun(t, "xz", []byte(payload))

	for _, name := range []string{"data.lzma", "data.tlz"} {
		file := filepath.Join(t.TempDir(), name)
		writeFile(t, file, stream)

		status, _, stderr := run(t, "xz", nil, "-d", file)
		if status != filecodec.ExitWarning || !strings.Contains(stderr, name+": unknown suffix -- ignored") {
			t.Errorf("%s: exit %d, stderr %q", name, status, stderr)
		}

		if got := readFile(t, file); got != stream {
			t.Errorf("%s: the input holds %q", name, got)
		}

		mustRun(t, "xz", nil, "-k", file)

		if got := decode(t, []byte(readFile(t, file+".xz"))); got != stream {
			t.Errorf("%s.xz decodes to %q", name, got)
		}
	}
}

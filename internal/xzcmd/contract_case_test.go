package xzcmd_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractSuffixIsMatchedAsWritten: xz knows its suffixes in lower
// case only. DATA.XZ has an unknown suffix: it is not decompressed,
// status 2, and compressing it adds .xz. A name that is a suffix and
// nothing else has no name to give its counterpart: an unknown suffix too.
func TestContractSuffixIsMatchedAsWritten(t *testing.T) {
	t.Parallel()

	stream := mustRun(t, "xz", []byte(payload))

	for _, name := range []string{"DATA.XZ", "data.Xz", "DATA.TXZ", ".xz", ".txz"} {
		dir := t.TempDir()
		file := filepath.Join(dir, name)
		writeFile(t, file, stream)

		status, _, stderr := run(t, "xz", nil, "-d", file)
		if status != filecodec.ExitWarning || !strings.Contains(stderr, name+": unknown suffix -- ignored") {
			t.Errorf("%s: exit %d, stderr %q", name, status, stderr)
		}

		if got := readFile(t, file); got != stream {
			t.Errorf("%s: the input holds %q", name, got)
		}

		if entries, err := filepath.Glob(filepath.Join(dir, "*")); err != nil || len(entries) > 1 {
			t.Errorf("%s: the directory holds %v (%v)", name, entries, err)
		}
	}

	file := filepath.Join(t.TempDir(), "DATA.XZ")
	writeFile(t, file, payload)
	mustRun(t, "xz", nil, "-k", file)

	if got := decode(t, []byte(readFile(t, file+".xz"))); got != payload {
		t.Errorf("DATA.XZ.xz decodes to %q", got)
	}
}

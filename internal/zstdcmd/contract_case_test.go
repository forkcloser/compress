package zstdcmd_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractSuffixIsMatchedAsWritten: zstd knows its suffixes in lower
// case only. DATA.ZST has an unknown suffix: it is not decompressed,
// status 1, and compressing it adds .zst. A name that is a suffix and
// nothing else has no name to give its counterpart: an unknown suffix too.
func TestContractSuffixIsMatchedAsWritten(t *testing.T) {
	t.Parallel()

	stream := mustRun(t, "zstd", []byte(payload))

	for _, name := range []string{"DATA.ZST", "data.Zst", "DATA.TZST", ".zst", ".tzst"} {
		dir := t.TempDir()
		file := filepath.Join(dir, name)
		writeFile(t, file, stream)

		status, _, stderr := run(t, "zstd", nil, "-d", file)
		if status != filecodec.ExitError || !strings.Contains(stderr, name+": unknown suffix -- ignored") {
			t.Errorf("%s: exit %d, stderr %q", name, status, stderr)
		}

		if got := readFile(t, file); got != stream {
			t.Errorf("%s: the input holds %q", name, got)
		}

		if entries, err := filepath.Glob(filepath.Join(dir, "*")); err != nil || len(entries) > 1 {
			t.Errorf("%s: the directory holds %v (%v)", name, entries, err)
		}
	}

	file := filepath.Join(t.TempDir(), "DATA.ZST")
	writeFile(t, file, payload)
	mustRun(t, "zstd", nil, "-k", file)

	if got := decode(t, []byte(readFile(t, file+".zst"))); got != payload {
		t.Errorf("DATA.ZST.zst decodes to %q", got)
	}
}

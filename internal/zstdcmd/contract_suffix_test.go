package zstdcmd_test

import (
	"path/filepath"
	"testing"
)

// TestContractSuffixedInputIsCompressed: zstd compresses a file whatever
// its name. One that already ends in a suffix of its own gets another:
// data.zst becomes data.zst.zst, status 0.
func TestContractSuffixedInputIsCompressed(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"data.zst", "data.tzst"} {
		file := filepath.Join(t.TempDir(), name)
		writeFile(t, file, payload)

		mustRun(t, "zstd", nil, file)

		if got := decode(t, []byte(readFile(t, file+".zst"))); got != payload {
			t.Errorf("%s.zst decodes to %q", name, got)
		}
	}
}

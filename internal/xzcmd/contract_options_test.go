package xzcmd_test

import (
	"testing"
)

// TestContractXzsOptions: the options gzip or zstd lack are xz's all the
// same: -z, --compress, --best and --to-stdout.
func TestContractXzsOptions(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"-z", "--compress", "--best", "--to-stdout"} {
		if got := decode(t, []byte(mustRun(t, "xz", []byte(payload), flag))); got != payload {
			t.Errorf("xz %s: %q", flag, got)
		}
	}
}

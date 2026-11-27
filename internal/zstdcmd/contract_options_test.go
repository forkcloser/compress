package zstdcmd_test

import (
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractNotZstdsOptions: --best and --to-stdout are gzip's and xz's.
// zstd has neither, and refuses them as it does any option it does not
// know. -z, --compress and --fast are its own.
func TestContractNotZstdsOptions(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"--best", "--to-stdout"} {
		status, stdout, stderr := run(t, "zstd", []byte(payload), flag)
		if status != filecodec.ExitError || stdout != "" ||
			!strings.Contains(stderr, "unrecognized option '"+flag+"'") {
			t.Errorf("zstd %s: exit %d, stdout %q, stderr %q", flag, status, stdout, stderr)
		}
	}

	for _, flag := range []string{"-z", "--compress", "--fast"} {
		if got := decode(t, []byte(mustRun(t, "zstd", []byte(payload), flag))); got != payload {
			t.Errorf("zstd %s: %q", flag, got)
		}
	}
}

package gzipcmd_test

import (
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractNotGzipsOptions: -z and --compress are xz's and zstd's.
// gzip has neither, and refuses them as it does any option it does not
// know, alone or in a cluster, whole or by a prefix.
func TestContractNotGzipsOptions(t *testing.T) {
	t.Parallel()

	for args, want := range map[string]string{
		"-z":         "invalid option -- 'z'",
		"-cz":        "invalid option -- 'z'",
		"--compress": "unrecognized option '--compress'",
		"--comp":     "unrecognized option '--comp'",
	} {
		status, stdout, stderr := run(t, "gzip", []byte(payload), args)
		if status != filecodec.ExitError || stdout != "" || !strings.Contains(stderr, want) {
			t.Errorf("gzip %s: exit %d, stdout %q, stderr %q", args, status, stdout, stderr)
		}
	}
}

package gzipcmd_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/filecodec"
)

// TestContractSuffixAloneIsNoName: a name that is a suffix and nothing else
// has no name to give its counterpart: gzip takes it for an unknown suffix,
// status 2, and leaves it.
func TestContractSuffixAloneIsNoName(t *testing.T) {
	t.Parallel()

	stream := mustRun(t, "gzip", []byte(payload))

	for _, name := range []string{".gz", ".tgz", "-gz"} {
		dir := t.TempDir()
		file := filepath.Join(dir, name)
		writeFile(t, file, stream)

		// The name after --: one of them begins with a dash.
		status, _, stderr := run(t, "gzip", nil, "-d", "--", file)
		if status != filecodec.ExitWarning || !strings.Contains(stderr, name+": unknown suffix -- ignored") {
			t.Errorf("%s: exit %d, stderr %q", name, status, stderr)
		}

		if got := readFile(t, file); got != stream {
			t.Errorf("%s: the input holds %q", name, got)
		}
	}
}

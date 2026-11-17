package xzcmd_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractExistingCounterpartIsAnError: for xz a counterpart that is
// already there, without --force, is an error and not a warning: status 1,
// in both directions, and --quiet, which silences warnings, does not
// silence it. The file in the way and the input are left as they were.
func TestContractExistingCounterpartIsAnError(t *testing.T) {
	t.Parallel()

	for direction, names := range map[string][2]string{
		"compressing":   {"data", "data.xz"},
		"decompressing": {"data.xz", "data"},
	} {
		for _, quiet := range []string{"-k", "-kq"} {
			dir := t.TempDir()
			input, target := filepath.Join(dir, names[0]), filepath.Join(dir, names[1])

			stream := mustRun(t, "xz", []byte(payload))
			writeFile(t, input, stream)
			writeFile(t, target, "in the way")

			args := []string{quiet, input}
			if direction == "decompressing" {
				args = append([]string{"-d"}, args...)
			}

			status, _, stderr := run(t, "xz", nil, args...)
			if status != filecodec.ExitError || !strings.Contains(stderr, names[1]+": ") {
				t.Errorf("%s, %s: exit %d, stderr %q", direction, quiet, status, stderr)
			}

			if got := readFile(t, target); got != "in the way" {
				t.Errorf("%s, %s: the file in the way holds %q", direction, quiet, got)
			}

			if got := readFile(t, input); got != stream {
				t.Errorf("%s, %s: the input holds %q", direction, quiet, got)
			}
		}
	}
}

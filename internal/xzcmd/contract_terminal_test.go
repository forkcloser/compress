package xzcmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
	"github.com/forkcloser/go-compress/internal/xzcmd"
)

// TestContractNullDeviceIsNotATerminal: the refusal to write compressed
// data is for a terminal. The null device is a character device and not
// one: `xz > /dev/null` and `xz -c file > /dev/null` compress into it.
func TestContractNullDeviceIsNotATerminal(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "data")
	writeFile(t, file, payload)

	for name, args := range map[string][]string{"standard input": nil, "a file": {"-c", file}} {
		null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}

		var diag bytes.Buffer

		status := xzcmd.Run("xz", args, bytes.NewReader([]byte(payload)), null, &diag, "test")
		if status != filecodec.ExitOK || diag.Len() != 0 {
			t.Errorf("%s to %s: exit %d, stderr %q", name, os.DevNull, status, diag.String())
		}

		if err := null.Close(); err != nil {
			t.Error(err)
		}
	}
}

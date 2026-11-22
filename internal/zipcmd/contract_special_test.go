package zipcmd_test

import (
	"os"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/zipcmd"
)

// TestContractDeviceIsNotAnInput: a device named as an input is not opened:
// it is skipped with a warning, the other inputs are stored and the status
// is 0; when it was the only input there is nothing to do.
func TestContractDeviceIsNotAnInput(t *testing.T) {
	tree(t)

	warning := "\tzip warning: ignoring special file: " + os.DevNull + "\n"

	status, _, stderr := run(t, "", "-q", "null.zip", os.DevNull, "d/a.txt")
	if status != zipcmd.ExitOK || stderr != warning {
		t.Errorf("exit %d, stderr %q, want 0 and %q", status, stderr, warning)
	}

	if got := strings.Join(names(readArchiveFile(t, "null.zip")), " "); got != "d/a.txt" {
		t.Errorf("the archive holds %q, want \"d/a.txt\"", got)
	}

	status, _, stderr = run(t, "", "-q", "only.zip", os.DevNull)
	if status != zipcmd.ExitNothing || !strings.HasPrefix(stderr, warning) {
		t.Errorf("the device alone: exit %d, stderr %q, want %d", status, stderr, zipcmd.ExitNothing)
	}
}

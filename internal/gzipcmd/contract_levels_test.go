package gzipcmd_test

import (
	"testing"
)

// TestContractEveryDigitIsALevel: in gzip each digit is an option of its
// own and the last one decides: -19 is -1 then -9, the stream -9 writes,
// and -91 the one -1 writes. Only a tool whose levels run past 9 reads a
// run of digits as one number.
func TestContractEveryDigitIsALevel(t *testing.T) {
	t.Parallel()

	one, nine := mustRun(t, "gzip", []byte(payload), "-1"), mustRun(t, "gzip", []byte(payload), "-9")
	if one == nine {
		t.Fatal("-1 and -9 write the same stream: nothing to tell them apart by")
	}

	for args, want := range map[string]string{"-19": nine, "-91": one, "-c19": nine, "-1c9k1": one} {
		status, stdout, stderr := run(t, "gzip", []byte(payload), args)
		if stdout != want {
			t.Errorf("%s: exit %d, stderr %q, and not the stream of its last digit", args, status, stderr)
		}
	}
}

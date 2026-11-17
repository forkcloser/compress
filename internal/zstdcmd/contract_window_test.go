package zstdcmd_test

import (
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/filecodec"
)

// declaring is a zstd frame of one raw block, "data\n", that declares the
// window the descriptor byte names: 0x88 is 128 MiB, 0x89 an eighth more.
func declaring(descriptor byte) []byte {
	return append([]byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, descriptor, 0x29, 0x00, 0x00}, "data\n"...)
}

// TestContractWindowIsHeldToZstdsLimit: a frame may declare a window of
// 128 MiB, which the decoder takes before it decodes a byte, and under
// --long=N one of two to the power N, be that more or less; of several
// --long the first is the one, and a bare one is 27: all as zstd has it. A
// frame over the limit is refused with the option that allows it; a --long
// zstd does not take is a usage error; compression takes --long and writes
// a stream that reads.
func TestContractWindowIsHeldToZstdsLimit(t *testing.T) {
	t.Parallel()

	atLimit, over := declaring(0x88), declaring(0x89)

	for _, row := range []struct {
		frame []byte
		args  []string
		read  bool
	}{
		{atLimit, []string{"-dc"}, true},
		{atLimit, []string{"-dc", "--long"}, true},
		{atLimit, []string{"-dc", "--long=26"}, false},
		{over, []string{"-dc"}, false},
		{over, []string{"-dc", "--long"}, false},
		{over, []string{"-dc", "--long=28"}, true},
		{over, []string{"-dc", "--long=28", "--long=27"}, true},
		{over, []string{"-dc", "--long=27", "--long=28"}, false},
		{over, []string{"-t"}, false},
	} {
		status, stdout, stderr := run(t, "zstd", row.frame, row.args...)

		if row.read && (status != filecodec.ExitOK || stdout != "data\n") {
			t.Errorf("zstd %v: exit %d, stdout %q, stderr %q; want it read", row.args, status, stdout, stderr)
		}

		if !row.read && (status != filecodec.ExitError || stdout != "" || !strings.Contains(stderr, "--long=N")) {
			t.Errorf("zstd %v: exit %d, stdout %q, stderr %q; want it refused", row.args, status, stdout, stderr)
		}
	}

	for _, long := range []string{"--long=9", "--long=32", "--long=x"} {
		if status, _, stderr := run(t, "zstd", atLimit, "-dc", long); status != filecodec.ExitError ||
			!strings.Contains(stderr, "--long") {
			t.Errorf("zstd -dc %s: exit %d, stderr %q", long, status, stderr)
		}
	}

	// The value is the option's own: the next word is a file.
	if status, _, stderr := run(t, "zstd", atLimit, "-dc", "--long", "30"); status != filecodec.ExitError ||
		!strings.Contains(stderr, "30") {
		t.Errorf("zstd -dc --long 30: exit %d, stderr %q", status, stderr)
	}

	compressed := mustRun(t, "zstd", []byte(payload), "--long=30")
	if got := mustRun(t, "zstd", []byte(compressed), "-dc"); got != payload {
		t.Errorf("compressed under --long=30, read back: %q", got)
	}
}

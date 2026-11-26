package tarcmd_test

import (
	"archive/tar"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/tarcmd"
)

// longWindow is `tar -cf - hello.txt | zstd --long=30`, by zstd 1.5.7: one
// member in a frame that declares a window of 1 GiB (window descriptor
// 0xa0), which is what zstd declares under --long=30 for an input it reads
// from a pipe, whatever its size.
func longWindow(t *testing.T) []byte {
	t.Helper()

	archive, err := hex.DecodeString("28b52ffd04a0b50200c40268656c6c6f2e747874003030303036343400303030303600003031" +
		"313034350020300075737461720030000a0f00fc801f042618bd0cdd00d10609d0c34700ad007e836400480e440083aea0801b8e6ea2" +
		"20014891f0497e")
	if err != nil {
		t.Fatal(err)
	}

	return archive
}

// declaring is a tar archive of one empty member, hello.txt, in a zstd
// frame that declares the window the descriptor byte names: 0x88 is
// 128 MiB, 0x89 an eighth more.
func declaring(t *testing.T, descriptor byte) []byte {
	t.Helper()

	plain := crafted(t, &tar.Header{Name: "hello.txt", Typeflag: tar.TypeReg, Mode: 0o644})
	if len(plain) != 2048 {
		t.Fatalf("the archive is %d bytes", len(plain))
	}

	// One raw block, the last: its three-byte header is the size, the type
	// and the last-block bit.
	frame := []byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, descriptor, byte(len(plain)<<3 | 1), byte(len(plain) >> 5), 0}

	return append(frame, plain...)
}

// TestContractZstdLongWindow: a zstd frame may declare a window of 128 MiB,
// and under --use-compress-program with zstd's --long=N one of two to the
// power N, be that more or less; of several --long the first is the one; a
// bare --long is --long=27. An archive zstd wrote with --long=30 is so read
// when the program is given that --long, as actions/cache gives it, and
// refused without it, the diagnostic naming --long. A --long zstd does not
// take is a usage error; another compressor's arguments are not read; of
// several programs the last is the one.
func TestContractZstdLongWindow(t *testing.T) {
	t.Parallel()

	const listed = "hello.txt\n"

	long, atLimit, over := longWindow(t), declaring(t, 0x88), declaring(t, 0x89)

	for _, row := range []struct {
		archive []byte
		program string
		read    bool
	}{
		{long, "unzstd --long=30", true},
		{long, "zstd -d --long=30", true},
		{long, "zstdmt --long=30", true},
		{long, "unzstd --long=31", true},
		{long, "unzstd --long=30 --long=27", true},
		{long, "unzstd --long=27 --long=30", false},
		{long, "unzstd", false},
		{long, "unzstd --long", false},
		{long, "unzstd --long=29", false},
		{atLimit, "unzstd", true},
		{atLimit, "unzstd --long", true},
		{atLimit, "unzstd --long=27", true},
		{atLimit, "unzstd --long=26", false},
		{atLimit, "unzstd --long=10", false},
		{over, "unzstd", false},
		{over, "unzstd --long=28", true},
	} {
		status, out, stderr := run(t, row.archive, "-tf", "-", "--use-compress-program", row.program)

		if row.read && (status != tarcmd.ExitOK || out != listed) {
			t.Errorf("-I %q: exit %d, stdout %q, stderr %q; want it read", row.program, status, out, stderr)
		}

		if !row.read && (status != tarcmd.ExitError || out != "" || !strings.Contains(stderr, "--long=N")) {
			t.Errorf("-I %q: exit %d, stdout %q, stderr %q; want it refused", row.program, status, out, stderr)
		}
	}

	// Without a program the limit is the default, by --zstd and by magic.
	for _, args := range [][]string{{"-tf", "-"}, {"-tf", "-", "--zstd"}} {
		if out, _ := mustRun(t, atLimit, args...); out != listed {
			t.Errorf("tar %q at the limit lists %q", args, out)
		}

		for _, archive := range [][]byte{over, long} {
			status, out, stderr := run(t, archive, args...)
			if status != tarcmd.ExitError || out != "" || !strings.Contains(stderr, "--long=N") {
				t.Errorf("tar %q: exit %d, stdout %q, stderr %q", args, status, out, stderr)
			}
		}
	}

	for _, long := range []string{"--long=9", "--long=32", "--long=", "--long=x", "--long30"} {
		status, _, stderr := run(t, atLimit, "-tf", "-", "--use-compress-program", "unzstd "+long)
		if status != tarcmd.ExitUsage || !strings.Contains(stderr, long) {
			t.Errorf("-I unzstd %s: exit %d, stderr %q", long, status, stderr)
		}
	}

	// Of several programs the last is the one, with its own --long or none.
	status, out, stderr := run(t, long, "-tf", "-", "--use-compress-program", "unzstd --long=30",
		"--use-compress-program", "zstd")
	if status != tarcmd.ExitError || out != "" || !strings.Contains(stderr, "--long=N") {
		t.Errorf("-I 'unzstd --long=30' -I zstd: exit %d, stdout %q, stderr %q", status, out, stderr)
	}

	gzipped, _ := mustRun(t, nil, "-czf", "-", "-T", os.DevNull)
	if _, stderr := mustRun(t, []byte(gzipped), "-tf", "-", "--use-compress-program", "gzip --long=x"); stderr != "" {
		t.Errorf("-I 'gzip --long=x': %q", stderr)
	}
}

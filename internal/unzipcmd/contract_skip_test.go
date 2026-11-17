package unzipcmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/forkcloser/compress/internal/unzipcmd"
)

// The lines Info-ZIP skips a member with.
const (
	skipMethod   = "   skipping: odd                     unsupported compression method 99\n"
	skipPassword = "   skipping: secret                  unable to get password\n"
)

// unreadable writes two archives, each with a member this command cannot
// read and one it can: "odd" by a method nothing here decompresses, in the
// first, and "secret", whose data is encrypted, in the second.
func unreadable(t *testing.T, dir string) (method, encrypted string) {
	t.Helper()

	odd := stored("odd", "theirs")
	odd.Method = 99

	secret := stored("secret", "theirs")
	secret.Flags |= 0x1

	method = buildRaw(t, filepath.Join(dir, "method.zip"), raw{odd, "theirs"}, raw{stored("fine", "fine\n"), "fine\n"})
	encrypted = buildRaw(t, filepath.Join(dir, "encrypted.zip"),
		raw{stored("plain", "plain\n"), "plain\n"}, raw{secret, "twelve bytes" + "theirs"})

	return method, encrypted
}

// TestContractSkippedMemberKeepsFile: a member this command cannot read at
// all, its data encrypted or compressed by a method it has no decompressor
// for, is skipped in Info-ZIP's words before anything is touched. Under -o
// the file in its place stays as it was, the other members are extracted,
// and the run ends 81 for the method, 5 for the password nobody could be
// asked for. -q drops the line for the method and keeps the password's.
func TestContractSkippedMemberKeepsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	method, encrypted := unreadable(t, dir)

	for _, tc := range []struct {
		name    string
		args    []string
		inPlace string
		other   string
		status  int
		stderr  string
	}{
		{"method", []string{"-o", method}, "odd", "fine", unzipcmd.ExitUnsupported, skipMethod},
		{"method quiet", []string{"-qo", method}, "odd", "fine", unzipcmd.ExitUnsupported, ""},
		{"encrypted", []string{"-o", encrypted}, "secret", "plain", unzipcmd.ExitPassword, skipPassword},
		{"encrypted quiet", []string{"-qo", encrypted}, "secret", "plain", unzipcmd.ExitPassword, skipPassword},
		{"encrypted, no -o", []string{"-q", encrypted}, "secret", "plain", unzipcmd.ExitPassword, skipPassword},
	} {
		out := filepath.Join(dir, tc.name)
		mine := filepath.Join(out, tc.inPlace)

		if err := os.Mkdir(out, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(mine, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}

		status, _, stderr := run(t, append(tc.args, "-d", out)...)
		if status != tc.status || stderr != tc.stderr {
			t.Errorf("%s: exit %d, stderr %q, want %d %q", tc.name, status, stderr, tc.status, tc.stderr)
		}

		if got, err := os.ReadFile(mine); err != nil || string(got) != "mine" {
			t.Errorf("%s: the file in the member's place: %q %v", tc.name, got, err)
		}

		if got := readFile(t, filepath.Join(out, tc.other)); got != tc.other+"\n" {
			t.Errorf("%s: the member beside it: %q", tc.name, got)
		}
	}
}

// TestContractSkippedMemberTestAndPipe: -t skips the same members on
// standard output, counts the ones skipped for their method apart from the
// ones it tested and ends 81, and calls the password an error, 5; -p writes
// the members it can read and nothing of the others.
func TestContractSkippedMemberTestAndPipe(t *testing.T) {
	t.Parallel()

	method, encrypted := unreadable(t, t.TempDir())

	const skippedLine = "1 file skipped because of unsupported compression or encoding.\n"

	for _, tc := range []struct {
		args   []string
		status int
		stdout string
		stderr string
	}{
		{
			[]string{"-t", method},
			unzipcmd.ExitUnsupported,
			"Archive:  " + method + "\n" + skipMethod + "    testing: fine                     OK\n" +
				"No errors detected in " + method + " for the 1 file tested.\n" + skippedLine, "",
		},
		{
			[]string{"-qt", method},
			unzipcmd.ExitUnsupported,
			"No errors detected in " + method + " for the 1 file tested.\n" + skippedLine, "",
		},
		{
			[]string{"-t", method, "odd"},
			unzipcmd.ExitUnsupported,
			"Archive:  " + method + "\n" + skipMethod + "Caution:  zero files tested in " + method + ".\n" + skippedLine, "",
		},
		{
			[]string{"-t", encrypted},
			unzipcmd.ExitPassword,
			"Archive:  " + encrypted + "\n    testing: plain                    OK\n" + skipPassword +
				"At least one error was detected in " + encrypted + ".\n", "",
		},
		{[]string{"-p", method}, unzipcmd.ExitUnsupported, "fine\n", ""},
		{[]string{"-p", encrypted}, unzipcmd.ExitPassword, "plain\n", skipPassword},
	} {
		status, stdout, stderr := run(t, tc.args...)
		if status != tc.status || stdout != tc.stdout || stderr != tc.stderr {
			t.Errorf("unzip %v: exit %d\nstdout %q\nstderr %q\nwant %d\nstdout %q\nstderr %q",
				tc.args, status, stdout, stderr, tc.status, tc.stdout, tc.stderr)
		}
	}
}

// TestContractSkippedMethodAsInfoZIP: the status and the two streams for a
// member of an unsupported method are the Info-ZIP unzip's, extracting over
// a file in its place, testing and piping. The encrypted member is not put
// to that tool: with a terminal to open it would ask it for the password.
func TestContractSkippedMethodAsInfoZIP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	method, _ := unreadable(t, dir)

	for _, args := range [][]string{
		{"-o", method, "-d", filepath.Join(dir, "out")},
		{"-qo", method, "-d", filepath.Join(dir, "out")},
		{"-t", method},
		{"-qt", method},
		{"-t", method, "odd"},
		{"-p", method},
	} {
		wantStatus, wantStdout, wantStderr := theirs(t, args...)

		// The line for a member extracted is padded as Info-ZIP pads it by
		// another contract; this one is about the member skipped.
		status, stdout, stderr := run(t, args...)
		if status != wantStatus || trimLines(stdout) != trimLines(wantStdout) || stderr != wantStderr {
			t.Errorf("unzip %v: exit %d\nstdout %q\nstderr %q\nInfo-ZIP: exit %d\nstdout %q\nstderr %q",
				args, status, stdout, stderr, wantStatus, wantStdout, wantStderr)
		}
	}
}

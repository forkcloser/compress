package unzipcmd_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/unzipcmd"
)

// The lines for a pattern that matched nothing, and what a listing of
// nothing is.
const (
	unmatched    = "caution: filename not matched:  nope\n"
	unmatchedToo = "caution: filename not matched:  nada\n"
	emptyListing = "  Length      Date    Time    Name\n---------  ---------- -----   ----\n" +
		"---------                     -------\n        0                     0 files\n"
)

// unmatchedRun is one invocation with a pattern that matches nothing, and
// what it ends with and writes.
type unmatchedRun struct {
	args   []string
	status int
	stdout string
	stderr string
}

// unmatchedRuns are the invocations on the pair's archive, extracting into
// out. No line of theirs names out: what is extracted there is extracted
// quietly.
func unmatchedRuns(archive, out string) []unmatchedRun {
	const (
		tested  = "    testing: x                        OK\n"
		listing = "  Length      Date    Time    Name\n---------  ---------- -----   ----\n" +
			"        1  05-06-2024 07:08   x\n---------                     -------\n" +
			"        1                     1 file\n"
	)

	opened := "Archive:  " + archive + "\n"
	failed := "At least one error was detected in " + archive + ".\n"

	return []unmatchedRun{
		{[]string{"-t", archive, "x", "nope"}, unzipcmd.ExitNoMatch, opened + tested + unmatched + failed, ""},
		{[]string{"-t", archive, "nope"}, unzipcmd.ExitNoMatch, opened + unmatched + failed, ""},
		{[]string{"-qt", archive, "x", "nope"}, unzipcmd.ExitNoMatch, unmatched + failed, ""},
		{[]string{"-p", archive, "x", "nope"}, unzipcmd.ExitNoMatch, "1", unmatched},
		{[]string{"-p", archive, "nope"}, unzipcmd.ExitNoMatch, "", unmatched},
		{[]string{"-q", archive, "nope", "x", "nada", "-d", out}, unzipcmd.ExitNoMatch, "", unmatched + unmatchedToo},
		{[]string{archive, "nope", "-d", out + "-none"}, unzipcmd.ExitNoMatch, opened, unmatched},
		{[]string{"-l", archive, "x", "nope"}, unzipcmd.ExitOK, opened + listing, ""},
		{[]string{"-l", archive, "nope"}, unzipcmd.ExitNoMatch, opened + emptyListing, ""},
	}
}

// excludedRuns are the invocations on the pair's archive where an exclude
// takes a member a pattern named: the pattern has matched, so no caution,
// and the status is 11 only when nothing is left to work on.
func excludedRuns(archive, out string) []unmatchedRun {
	opened := "Archive:  " + archive + "\n"
	none := "Caution:  zero files tested in " + archive + ".\n"
	failed := "At least one error was detected in " + archive + ".\n"

	return []unmatchedRun{
		{[]string{"-p", archive, "x", "y", "-x", "x"}, unzipcmd.ExitOK, "2", ""},
		{[]string{"-q", archive, "x", "y", "-x", "x", "-d", out}, unzipcmd.ExitOK, "", ""},
		{[]string{"-p", archive, "x", "-x", "x"}, unzipcmd.ExitNoMatch, "", ""},
		{[]string{"-q", archive, "x", "-x", "x", "-d", out}, unzipcmd.ExitNoMatch, "", ""},
		{[]string{"-q", archive, "-x", "*", "-d", out}, unzipcmd.ExitNoMatch, "", ""},
		{[]string{"-t", archive, "x", "-x", "x"}, unzipcmd.ExitNoMatch, opened + none, ""},
		{[]string{"-t", archive, "x", "nope", "-x", "x"}, unzipcmd.ExitNoMatch, opened + unmatched + failed, ""},
		{[]string{"-l", archive, "x", "-x", "x"}, unzipcmd.ExitNoMatch, opened + emptyListing, ""},
	}
}

// monthFirst spells a listing's dates month first, as this command and
// Info-ZIP's default do. The order is a build option of that tool, and a
// distribution may ship it year first.
func monthFirst(listing string) string {
	yearFirst := regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2}) (\d{2}:\d{2})`)

	return yearFirst.ReplaceAllString(listing, "$2-$3-$1 $4")
}

// pair is an archive of two files with only the DOS times, which read the
// same wherever they are listed.
func pair(t *testing.T) string {
	t.Helper()

	zone := time.Local //nolint:gosmopolitan // the DOS fields are local time
	when := time.Date(2024, time.May, 6, 7, 8, 10, 0, zone)

	return build(t, filepath.Join(t.TempDir(), "pair.zip"),
		member{name: "x", data: "1", dosOnly: true, when: when},
		member{name: "y", data: "2", dosOnly: true, when: when})
}

// TestContractUnmatchedPattern: a member pattern that matches nothing is
// status 11 when testing and piping as it is when extracting, and its
// caution comes after the members, on standard error: under -p standard
// output is the members' content alone. -t, which has one stream, writes
// it there and calls it an error. A listing says nothing of it, and is 11
// only when it lists nothing. The -d directory is made even for nothing.
func TestContractUnmatchedPattern(t *testing.T) {
	t.Parallel()

	archive := pair(t)
	out := filepath.Join(t.TempDir(), "out")

	for _, tc := range unmatchedRuns(archive, out) {
		status, stdout, stderr := run(t, tc.args...)
		if status != tc.status || stdout != tc.stdout || stderr != tc.stderr {
			t.Errorf("unzip %v: exit %d\nstdout %q\nstderr %q\nwant %d\nstdout %q\nstderr %q",
				tc.args, status, stdout, stderr, tc.status, tc.stdout, tc.stderr)
		}
	}

	if _, err := os.Stat(out + "-none"); err != nil {
		t.Errorf("the directory nothing was extracted into: %v", err)
	}
}

// TestContractExcludedMember: a pattern whose member an exclude takes has
// matched. The other members are worked on with status 0 and no caution;
// with none left the status is 11, still with no caution, and -t says it
// tested nothing.
func TestContractExcludedMember(t *testing.T) {
	t.Parallel()

	archive := pair(t)

	for _, tc := range excludedRuns(archive, filepath.Join(t.TempDir(), "out")) {
		status, stdout, stderr := run(t, tc.args...)
		if status != tc.status || stdout != tc.stdout || stderr != tc.stderr {
			t.Errorf("unzip %v: exit %d\nstdout %q\nstderr %q\nwant %d\nstdout %q\nstderr %q",
				tc.args, status, stdout, stderr, tc.status, tc.stdout, tc.stderr)
		}
	}

	// -t of what is left: the status and the member are the contract here,
	// the summary's words are not.
	status, stdout, stderr := run(t, "-t", archive, "x", "y", "-x", "x")
	if status != unzipcmd.ExitOK || stderr != "" || !strings.Contains(stdout, "testing: y") ||
		strings.Contains(stdout, "caution") {
		t.Errorf("unzip -t x y -x x: exit %d\nstdout %q\nstderr %q", status, stdout, stderr)
	}
}

// TestContractUnmatchedPatternOutranksSkipped: 11 for the pattern is the
// status over 81 for a member skipped for its method, and under 5 for one
// that is encrypted, as Info-ZIP settles them.
func TestContractUnmatchedPatternOutranksSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	method, encrypted := unreadable(t, dir)

	for _, tc := range []struct {
		args   []string
		status int
	}{
		{[]string{"-q", method, "odd", "nope", "-d", filepath.Join(dir, "out")}, unzipcmd.ExitNoMatch},
		{[]string{"-t", method, "odd", "nope"}, unzipcmd.ExitNoMatch},
		{[]string{"-p", method, "odd", "nope"}, unzipcmd.ExitNoMatch},
		{[]string{"-q", encrypted, "secret", "nope", "-d", filepath.Join(dir, "out")}, unzipcmd.ExitPassword},
		{[]string{"-t", encrypted, "secret", "nope"}, unzipcmd.ExitPassword},
		{[]string{"-p", encrypted, "secret", "nope"}, unzipcmd.ExitPassword},
	} {
		if status, _, _ := run(t, tc.args...); status != tc.status {
			t.Errorf("unzip %v: exit %d, want %d", tc.args, status, tc.status)
		}
	}
}

// TestContractUnmatchedPatternAsInfoZIP: the status and the two streams of
// each of those runs are the Info-ZIP unzip's.
func TestContractUnmatchedPatternAsInfoZIP(t *testing.T) {
	t.Parallel()

	archive := pair(t)

	// Each tool extracts into a directory of its own: neither finds the
	// other's file in its way.
	dir := t.TempDir()
	reference := unmatchedRuns(archive, filepath.Join(dir, "theirs"))

	for i, tc := range unmatchedRuns(archive, filepath.Join(dir, "ours")) {
		wantStatus, wantStdout, wantStderr := theirs(t, reference[i].args...)

		status, stdout, stderr := run(t, tc.args...)
		if status != wantStatus || stdout != monthFirst(wantStdout) || stderr != wantStderr {
			t.Errorf("unzip %v: exit %d\nstdout %q\nstderr %q\nInfo-ZIP: exit %d\nstdout %q\nstderr %q",
				tc.args, status, stdout, stderr, wantStatus, wantStdout, wantStderr)
		}
	}

	excluded := excludedRuns(archive, filepath.Join(dir, "theirs-left"))

	for i, tc := range excludedRuns(archive, filepath.Join(dir, "ours-left")) {
		wantStatus, wantStdout, wantStderr := theirs(t, excluded[i].args...)

		status, stdout, stderr := run(t, tc.args...)
		if status != wantStatus || stdout != monthFirst(wantStdout) || stderr != wantStderr {
			t.Errorf("unzip %v: exit %d\nstdout %q\nstderr %q\nInfo-ZIP: exit %d\nstdout %q\nstderr %q",
				tc.args, status, stdout, stderr, wantStatus, wantStdout, wantStderr)
		}
	}

	wantStatus, _, wantStderr := theirs(t, "-t", archive, "x", "y", "-x", "x")
	if status, _, stderr := run(t, "-t", archive, "x", "y", "-x", "x"); status != wantStatus || stderr != wantStderr {
		t.Errorf("unzip -t x y -x x: exit %d, stderr %q\nInfo-ZIP: exit %d, stderr %q",
			status, stderr, wantStatus, wantStderr)
	}

	_, theirErr := os.Stat(filepath.Join(dir, "theirs-none"))
	if _, err := os.Stat(filepath.Join(dir, "ours-none")); (err == nil) != (theirErr == nil) {
		t.Errorf("the directory nothing was extracted into: %v, Info-ZIP's: %v", err, theirErr)
	}

	method, _ := unreadable(t, t.TempDir())

	for _, args := range [][]string{{"-t", method, "odd", "nope"}, {"-p", method, "odd", "nope"}} {
		wantStatus, wantStdout, wantStderr := theirs(t, args...)

		status, stdout, stderr := run(t, args...)
		if status != wantStatus || stdout != wantStdout || stderr != wantStderr {
			t.Errorf("unzip %v: exit %d\nstdout %q\nstderr %q\nInfo-ZIP: exit %d\nstdout %q\nstderr %q",
				args, status, stdout, stderr, wantStatus, wantStdout, wantStderr)
		}
	}
}

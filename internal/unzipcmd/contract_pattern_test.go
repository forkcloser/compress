package unzipcmd_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/unzipcmd"
)

// listed is the names a short listing shows, in order, joined by spaces.
func listed(stdout string) string {
	var names []string

	for line := range strings.SplitSeq(stdout, "\n") {
		// A member's line is its length, date, time and name.
		if fields := strings.Fields(line); len(fields) == 4 && strings.Contains(fields[2], ":") {
			names = append(names, fields[3])
		}
	}

	return strings.Join(names, " ")
}

// patterns are member patterns and the fixture's names each selects.
func patterns() map[string]string {
	return map[string]string{
		"*":         "a.c d/a.c d/sub/b.txt d/sub/c.c",
		"*.c":       "a.c d/a.c d/sub/c.c",
		"**c":       "a.c d/a.c d/sub/c.c",
		"a.c*":      "a.c",
		"?.c":       "a.c",
		"d/?.c":     "d/a.c",
		"d/*b.txt":  "d/sub/b.txt",
		"d/*/?.*":   "d/sub/b.txt d/sub/c.c",
		"*/*/*.c":   "d/sub/c.c",
		"*a*.c":     "a.c d/a.c",
		"d/sub":     "",
		"?":         "",
		"*sub*sub*": "",
		"*.c?":      "",
	}
}

func named(t *testing.T) string {
	t.Helper()

	return build(t, filepath.Join(t.TempDir(), "names.zip"),
		member{name: "a.c", data: "1"}, member{name: "d/a.c", data: "2"},
		member{name: "d/sub/b.txt", data: "3"}, member{name: "d/sub/c.c", data: "4"})
}

// TestContractPatterns: in a member pattern "?" is any one character and
// "*" any run of them, a "/" included, none included; the whole name has to
// match, and a pattern that selects nothing is status 11.
func TestContractPatterns(t *testing.T) {
	t.Parallel()

	archive := named(t)

	for pattern, want := range patterns() {
		status, stdout, _ := run(t, "-l", archive, pattern)

		wantStatus := unzipcmd.ExitOK
		if want == "" {
			wantStatus = unzipcmd.ExitNoMatch
		}

		if got := listed(stdout); got != want || status != wantStatus {
			t.Errorf("%s selects %q, exit %d; want %q, exit %d", pattern, got, status, want, wantStatus)
		}
	}
}

// TestContractPatternsAsInfoZIP: each of those patterns selects what it
// selects for the Info-ZIP unzip.
func TestContractPatternsAsInfoZIP(t *testing.T) {
	t.Parallel()

	archive := named(t)

	for pattern := range patterns() {
		_, theirStdout, _ := theirs(t, "-l", archive, pattern)
		_, stdout, _ := run(t, "-l", archive, pattern)

		if got, want := listed(stdout), listed(theirStdout); got != want {
			t.Errorf("%s selects %q, Info-ZIP's %q", pattern, got, want)
		}
	}
}

// TestContractPatternTime: a pattern is matched against a name in a time
// that grows with the two lengths, not with every way the name can be
// split between the pattern's stars: a name is the archive's to choose, up
// to 65535 bytes of it.
func TestContractPatternTime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for _, tc := range []struct {
		name    string
		pattern string
	}{
		{strings.Repeat("a", 200), "*a*a*a*a*a*a*a*a*b"},
		{strings.Repeat("a/", 30000) + "b", "*/*/*.c"},
	} {
		archive := build(t, filepath.Join(dir, "long.zip"), member{name: tc.name, data: "1"})
		done := make(chan int)

		go func() {
			status, _, _ := run(t, "-l", archive, tc.pattern)
			done <- status
		}()

		select {
		case status := <-done:
			if status != unzipcmd.ExitNoMatch {
				t.Errorf("%s against a name of %d bytes: exit %d", tc.pattern, len(tc.name), status)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s against a name of %d bytes: still matching after 10 s", tc.pattern, len(tc.name))
		}
	}
}

package zipcmd_test

import (
	"os"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/zipcmd"
)

// TestContractRepeatedInput: an input named more than once, or named beside
// the directory -r takes it from, is stored once and the status is 0. Two
// different paths that map to one name in the archive are a usage error,
// status 16, and no archive is written: under -j, and for one file spelled
// two ways.
func TestContractRepeatedInput(t *testing.T) {
	tree(t)
	writeFile(t, "e/a.txt", "other\n")

	const walked = "d/ d/a.txt d/sub/ d/sub/b.txt"

	for _, tc := range []struct {
		archive string
		args    []string
		names   string
	}{
		{"twice.zip", []string{"-q", "twice.zip", "d/a.txt", "d/a.txt"}, "d/a.txt"},
		{"under.zip", []string{"-qr", "under.zip", "d", "d/a.txt"}, walked},
		{"again.zip", []string{"-qr", "again.zip", "d", "d"}, walked},
		{"slash.zip", []string{"-q", "slash.zip", "d", "d/"}, "d/"},
	} {
		status, _, stderr := run(t, "", tc.args...)
		if status != zipcmd.ExitOK || stderr != "" {
			t.Errorf("zip %v: exit %d, stderr %q, want 0 and nothing said", tc.args, status, stderr)

			continue
		}

		if got := strings.Join(names(readArchiveFile(t, tc.archive)), " "); got != tc.names {
			t.Errorf("zip %v: the archive holds %q, want %q", tc.args, got, tc.names)
		}
	}

	for _, tc := range []struct {
		archive string
		args    []string
		want    string
	}{
		{
			"junk.zip",
			[]string{"-qj", "junk.zip", "d/a.txt", "e/a.txt"},
			"\tzip warning: first full name: d/a.txt\n\tzip warning: second full name: e/a.txt\n" +
				"\tzip warning: name in zip file repeated: a.txt\n",
		},
		{
			"dot.zip",
			[]string{"-q", "dot.zip", "d/a.txt", "./d/a.txt"},
			"\tzip warning: first full name: d/a.txt\n\tzip warning: second full name: ./d/a.txt\n" +
				"\tzip warning: name in zip file repeated: d/a.txt\n",
		},
	} {
		status, _, stderr := run(t, "", tc.args...)
		if status != zipcmd.ExitUsage || !strings.HasPrefix(stderr, tc.want) ||
			!strings.HasSuffix(stderr, "zip error: Invalid command arguments (cannot repeat names in zip file)\n") {
			t.Errorf("zip %v: exit %d, stderr %q, want %d and %q", tc.args, status, stderr, zipcmd.ExitUsage, tc.want)
		}

		if _, err := os.Stat(tc.archive); err == nil {
			t.Errorf("zip %v: %s was written", tc.args, tc.archive)
		}
	}
}

package xzcmd_test

import (
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractMemoryLimitIsReadAndNotEnforced: -M and the --memlimit
// options take a limit in xz's grammar and enforce none: a limit of one
// byte compresses and decompresses all the same, and the help says so. A
// value xz refuses is refused, as a usage error: nothing is guessed from
// it.
func TestContractMemoryLimitIsReadAndNotEnforced(t *testing.T) {
	t.Parallel()

	stream := mustRun(t, "xz", []byte(payload))

	for _, limit := range []string{
		"1", "0", "max", "00010", " 1", "4GiB", "1k", "1K", "1kB", "1KiB", "1Mi", "1m", "1gB", "1G",
		"1%", "50%", "100%", "18446744073709551615", "18014398509481983KiB",
	} {
		for _, option := range [][]string{
			{"-M", limit},
			{"-M" + limit},
			{"--memlimit=" + limit},
			{"--memlimit", limit},
			{"--memlimit-compress=" + limit},
			{"--memlimit-decompress=" + limit},
		} {
			if got := decode(t, []byte(mustRun(t, "xz", []byte(payload), option...))); got != payload {
				t.Errorf("xz %v: %q", option, got)
			}

			if got := mustRun(t, "xz", []byte(stream), append([]string{"-d"}, option...)...); got != payload {
				t.Errorf("xz -d %v: %q", option, got)
			}
		}
	}

	for limit, want := range map[string]string{
		"bogus":  "bogus: Value is not a non-negative decimal integer",
		"":       ": Value is not a non-negative decimal integer",
		"-1":     "-1: Value is not a non-negative decimal integer",
		"MAX":    "MAX: Value is not a non-negative decimal integer",
		"%":      "%: Value is not a non-negative decimal integer",
		"1B":     "B: Invalid multiplier suffix",
		"1mib":   "mib: Invalid multiplier suffix",
		"1TiB":   "TiB: Invalid multiplier suffix",
		"1.5MiB": ".5MiB: Invalid multiplier suffix",
		//nolint:gocritic // mapKey: the space after the digit is the case
		"1 ":                   " : Invalid multiplier suffix",
		"0%":                   "must be in the range [1, 100]",
		"101%":                 "must be in the range [1, 100]",
		"18446744073709551616": "must be in the range [0, 18446744073709551615]",
		"17179869184GiB":       "must be in the range [0, 18446744073709551615]",
	} {
		for _, option := range []string{"--memlimit=", "--memlimit-compress=", "--memlimit-decompress=", "-M"} {
			if option == "-M" && limit == "" {
				continue // -M with nothing after it takes the next word
			}

			status, stdout, stderr := run(t, "xz", []byte(payload), option+limit)
			if status != filecodec.ExitError || stdout != "" || !strings.Contains(stderr, want) {
				t.Errorf("xz %s%s: exit %d, stdout %q, stderr %q", option, limit, status, stdout, stderr)
			}
		}
	}

	if help := mustRun(t, "xz", nil, "--help"); !strings.Contains(help, "-M, --memlimit=LIMIT") ||
		!strings.Contains(help, "no limit is enforced") {
		t.Errorf("the help does not say it:\n%s", help)
	}
}

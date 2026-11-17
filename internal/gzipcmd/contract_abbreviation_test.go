package gzipcmd_test

import (
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractPrefixIsResolvedAmongAllOfGzipsOptions: a long option given
// by a prefix is the one option of gzip's own table it begins, not the one
// of the options this command takes. A prefix gzip calls ambiguous is
// ambiguous here, with the options it could mean; one that names an option
// this command does not take is refused under that option's name; one that
// names an option it takes is that option.
func TestContractPrefixIsResolvedAmongAllOfGzipsOptions(t *testing.T) {
	t.Parallel()

	for flag, want := range map[string]string{
		"--s":         "option '--s' is ambiguous; possibilities: '--silent' '--stdout' '--suffix' '--synchronous'",
		"--l":         "option '--l' is ambiguous; possibilities: '--license' '--list' '--lzw'",
		"--r":         "option '--r' is ambiguous; possibilities: '--recursive' '--rsyncable'",
		"--b":         "option '--b' is ambiguous; possibilities: '--best' '--bits'",
		"--rec":       "unrecognized option '--recursive'",
		"--a":         "unrecognized option '--ascii'",
		"--list":      "unrecognized option '--list'",
		"--suffix=.x": "unrecognized option '--suffix'",
	} {
		status, stdout, stderr := run(t, "gzip", []byte(payload), flag)
		if status != filecodec.ExitError || stdout != "" || !strings.Contains(stderr, want) {
			t.Errorf("gzip %s: exit %d, stdout %q, stderr %q", flag, status, stdout, stderr)
		}
	}

	stream := mustRun(t, "gzip", []byte(payload))

	for _, flag := range []string{"--d", "--u"} {
		if got := mustRun(t, "gzip", []byte(stream), flag); got != payload {
			t.Errorf("gzip %s: %q", flag, got)
		}
	}

	for _, flag := range []string{"--st", "--k", "--be", "--na"} {
		if got, _ := decode(t, []byte(mustRun(t, "gzip", []byte(payload), flag))); got != payload {
			t.Errorf("gzip %s: %q", flag, got)
		}
	}
}

package xzcmd_test

import (
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/filecodec"
)

// TestContractPrefixIsResolvedAmongAllOfXzsOptions: a long option given by
// a prefix is the one option of xz's own table it begins, not the one of
// the options this command takes. A prefix xz calls ambiguous is ambiguous
// here, with the options it could mean; one that names an option this
// command does not take is refused under that option's name; one that
// names an option it takes is that option.
func TestContractPrefixIsResolvedAmongAllOfXzsOptions(t *testing.T) {
	t.Parallel()

	for flag, want := range map[string]string{
		"--s":    "option '--s' is ambiguous; possibilities: '--single-stream' '--sparc' '--stdout' '--suffix'",
		"--de":   "option '--de' is ambiguous; possibilities: '--decompress' '--delta'",
		"--b":    "option '--b' is ambiguous; possibilities: '--best' '--block-list' '--block-size'",
		"--no":   "option '--no' is ambiguous; possibilities: '--no-adjust' '--no-sparse' '--no-sync' '--no-warn'",
		"--li":   "unrecognized option '--list'",
		"--memo": "unrecognized option '--memory'",
		"--list": "unrecognized option '--list'",
	} {
		status, stdout, stderr := run(t, "xz", []byte(payload), flag)
		if status != filecodec.ExitError || stdout != "" || !strings.Contains(stderr, want) {
			t.Errorf("xz %s: exit %d, stdout %q, stderr %q", flag, status, stdout, stderr)
		}
	}

	stream := mustRun(t, "xz", []byte(payload))

	for _, flag := range []string{"--dec", "--unc"} {
		if got := mustRun(t, "xz", []byte(stream), flag); got != payload {
			t.Errorf("xz %s: %q", flag, got)
		}
	}

	for _, flag := range []string{"--std", "--k", "--ex", "--forc"} {
		if got := decode(t, []byte(mustRun(t, "xz", []byte(payload), flag))); got != payload {
			t.Errorf("xz %s: %q", flag, got)
		}
	}
}

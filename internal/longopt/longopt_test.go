package longopt_test

import (
	"testing"

	"github.com/forkcloser/compress/internal/longopt"
)

// TestResolve: a whole name wins over the longer names it begins, one
// prefix match is the option, none is unrecognized, several are ambiguous
// and listed sorted whatever order the names came in.
func TestResolve(t *testing.T) {
	t.Parallel()

	names := []string{"no-name", "name", "memlimit-decompress", "memlimit", "memlimit-compress", "verbose", "version"}

	for _, tc := range []struct {
		given, want, diag string
	}{
		{"memlimit", "memlimit", ""},
		{"memlimit-c", "memlimit-compress", ""},
		{"vers", "version", ""},
		{"bogus", "", "unrecognized option '--bogus'"},
		{"n", "", "option '--n' is ambiguous; possibilities: '--name' '--no-name'"},
		{
			"memlimit-", "",
			"option '--memlimit-' is ambiguous; possibilities: '--memlimit-compress' '--memlimit-decompress'",
		},
		{"ver", "", "option '--ver' is ambiguous; possibilities: '--verbose' '--version'"},
	} {
		got, diag := longopt.Resolve(tc.given, names)
		if got != tc.want || diag != tc.diag {
			t.Errorf("--%s: (%q, %q), want (%q, %q)", tc.given, got, diag, tc.want, tc.diag)
		}
	}
}

// Package longopt resolves a long option given by any unambiguous prefix of
// its name, as getopt_long does for GNU tar, gzip and xz, with glibc's words
// when it cannot.
package longopt

import (
	"fmt"
	"slices"
	"strings"
)

// Resolve is the option among names that given names: given itself when it
// is a whole name, else the one name it is a prefix of. When it is none,
// or several, the second result is the diagnostic, without the tool's
// prefix, and the first is empty.
func Resolve(given string, names []string) (name, diag string) {
	if slices.Contains(names, given) {
		return given, ""
	}

	var found []string

	for _, candidate := range names {
		if strings.HasPrefix(candidate, given) {
			found = append(found, candidate)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Sprintf("unrecognized option '--%s'", given)
	case 1:
		return found[0], ""
	}

	slices.Sort(found)

	for i, candidate := range found {
		found[i] = "'--" + candidate + "'"
	}

	return "", fmt.Sprintf("option '--%s' is ambiguous; possibilities: %s", given, strings.Join(found, " "))
}

package tarcmd

import (
	"path"
	"strings"
)

// excluded is --exclude as GNU tar matches it by default (unanchored): a
// pattern excludes a name when it matches the whole name, any tail that
// starts after a slash, or a leading directory of either, so a pattern that
// names a directory excludes what is under it too.
func excluded(patterns []string, name string) bool {
	name = strings.Trim(name, "/")

	for _, pattern := range patterns {
		for tail := name; ; {
			if matchesOrUnder(pattern, tail) {
				return true
			}

			_, rest, found := strings.Cut(tail, "/")
			if !found {
				break
			}

			tail = rest
		}
	}

	return false
}

// matchesOrUnder: the pattern matches name or one of its leading
// directories.
func matchesOrUnder(pattern, name string) bool {
	for prefix := name; prefix != ""; {
		if matched, _ := path.Match(pattern, prefix); matched { // a malformed pattern matches nothing
			return true
		}

		slash := strings.LastIndex(prefix, "/")
		if slash < 0 {
			break
		}

		prefix = prefix[:slash]
	}

	return false
}

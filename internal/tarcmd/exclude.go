package tarcmd

import (
	"strings"
	"unicode/utf8"
)

// excluded is --exclude as GNU tar matches it by default (unanchored): a
// pattern excludes a name when it matches the whole name, any tail that
// starts after a slash, or a leading directory of either, so a pattern that
// names a directory excludes what is under it too. The name keeps its
// leading slash, for a pattern that has one; a pattern that ends with a
// slash matches nothing.
func excluded(patterns []string, name string) bool {
	name = strings.TrimRight(name, "/")

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
		if fnmatch(pattern, prefix) {
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

// fnmatch reports whether name matches pattern as fnmatch(3) matches with no
// flag set, which is how GNU tar matches an exclusion: "*" is any run of
// characters and "?" any one, a slash as any other; a class is a set of
// characters, ranges and named classes, negated by "!" or "^"; a backslash
// takes the next character as itself, and one that ends the pattern matches
// nothing; a bracket that opens no class is itself. A character is a UTF-8
// sequence, and a named class holds what it holds in the C locale.
func fnmatch(pattern, name string) bool {
	// Where the last "*" is, and how much of the name it has taken: on a
	// mismatch it takes one character more and the match resumes after it.
	star, taken := -1, 0

	for inPattern, inName := 0, 0; inPattern < len(pattern) || inName < len(name); {
		if inPattern < len(pattern) && pattern[inPattern] == '*' {
			star, taken = inPattern, inName
			inPattern++

			continue
		}

		if inPattern < len(pattern) && inName < len(name) {
			if patternWidth, nameWidth, ok := element(pattern[inPattern:], name[inName:]); ok {
				inPattern += patternWidth
				inName += nameWidth

				continue
			}
		}

		if star < 0 || taken >= len(name) {
			return false
		}

		_, width := utf8.DecodeRuneInString(name[taken:])
		taken += width
		inPattern, inName = star+1, taken
	}

	return true
}

// element matches the pattern's first element, which is not "*", against
// the name's first character, and says how many bytes of each it took.
func element(pattern, name string) (inPattern, inName int, matched bool) {
	char, width := utf8.DecodeRuneInString(name)

	switch pattern[0] {
	case '?':
		return 1, width, true
	case '\\':
		if len(pattern) == 1 {
			return 0, 0, false
		}

		return 2, 1, pattern[1] == name[0]
	case '[':
		if end := classEnd(pattern); end > 0 {
			return end + 1, width, inClass(pattern[1:end], char)
		}
	}

	return 1, 1, pattern[0] == name[0]
}

// classEnd is the index of the "]" that closes the class pattern opens, or
// -1 when nothing closes it. A "]" that comes first, after the negation,
// is one of the class's characters.
func classEnd(pattern string) int {
	index := 1

	if index < len(pattern) && (pattern[index] == '!' || pattern[index] == '^') {
		index++
	}

	if index < len(pattern) && pattern[index] == ']' {
		index++
	}

	for index < len(pattern) {
		switch {
		case pattern[index] == '\\' && index+1 < len(pattern):
			index += 2
		case pattern[index] == ']':
			return index
		default:
			if _, width, ok := namedClass(pattern[index:]); ok {
				index += width
			} else {
				index++
			}
		}
	}

	return -1
}

// inClass reports whether char is in the class whose text, between the
// brackets, is class. A "-" stands for itself where it has no character
// on both sides.
func inClass(class string, char rune) bool {
	negated := class[0] == '!' || class[0] == '^'
	if negated {
		class = class[1:]
	}

	found := false

	for class != "" {
		if name, width, ok := namedClass(class); ok {
			found = found || named(name, char)
			class = class[width:]

			continue
		}

		low, width := classChar(class)
		class = class[width:]
		high := low

		if len(class) > 1 && class[0] == '-' {
			high, width = classChar(class[1:])
			class = class[1+width:]
		}

		found = found || (low <= char && char <= high)
	}

	return found != negated
}

// classChar is the character a class's text starts with, a backslash
// taking the next one as itself, and its width.
func classChar(class string) (rune, int) {
	escape := 0
	if class[0] == '\\' && len(class) > 1 {
		escape = 1
	}

	char, width := utf8.DecodeRuneInString(class[escape:])

	return char, escape + width
}

// namedClass is the name between "[:" and ":]" that text starts with, and
// the width of the whole.
func namedClass(text string) (name string, width int, ok bool) {
	rest, isNamed := strings.CutPrefix(text, "[:")
	if !isNamed {
		return "", 0, false
	}

	name, _, closed := strings.Cut(rest, ":]")
	if !closed {
		return "", 0, false
	}

	return name, len("[:") + len(name) + len(":]"), true
}

// del is the last of ASCII, a control character.
const del = 0x7f

// named reports whether char is in the class of that name in the C locale;
// a name that is no class holds nothing.
//
//nolint:gocognit // one case for each of POSIX's twelve classes: a table of them would say no more
func named(name string, char rune) bool {
	lower, upper, digit := char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9'
	graph := char > ' ' && char < del

	switch name {
	case "alpha":
		return lower || upper
	case "digit":
		return digit
	case "alnum":
		return lower || upper || digit
	case "lower":
		return lower
	case "upper":
		return upper
	case "xdigit":
		return digit || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')
	case "blank":
		return char == ' ' || char == '\t'
	case "space":
		return char == ' ' || (char >= '\t' && char <= '\r')
	case "cntrl":
		return char < ' ' || char == del
	case "graph":
		return graph
	case "print":
		return graph || char == ' '
	case "punct":
		return graph && !lower && !upper && !digit
	}

	return false
}

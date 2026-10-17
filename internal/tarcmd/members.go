package tarcmd

import (
	"strings"
)

// selection is what --extract and --list take when members are named, on
// the command line or in -T files: an entry whose name is one of them or
// lies under one, as GNU tar matches member names by default (anchored, no
// wildcards, a directory with everything under it). Names are compared
// without their trailing slashes, so "dir/" and "dir" name the same member.
// With no names, every entry is taken.
type selection struct {
	names   []string
	matched []bool
}

// newSelection reads the members' names; a -T list contributes one per
// line.
func newSelection(members []member) (*selection, error) {
	var names []string

	for _, entry := range members {
		if entry.list == "" {
			names = append(names, entry.name)

			continue
		}

		listed, err := readList(entry.list)
		if err != nil {
			return nil, err
		}

		names = append(names, listed...)
	}

	s := &selection{names: make([]string, len(names)), matched: make([]bool, len(names))}
	for i, name := range names {
		s.names[i] = strings.TrimRight(name, "/")
	}

	return s, nil
}

// take reports whether the entry named archiveName is selected, marking the
// members it matched.
func (s *selection) take(archiveName string) bool {
	if len(s.names) == 0 {
		return true
	}

	name := strings.TrimRight(archiveName, "/")
	taken := false

	for i, want := range s.names {
		if name == want || strings.HasPrefix(name, want+"/") {
			s.matched[i] = true
			taken = true
		}
	}

	return taken
}

// missing are the members no entry matched, as given.
func (s *selection) missing() []string {
	var out []string

	for i, matched := range s.matched {
		if !matched {
			out = append(out, s.names[i])
		}
	}

	return out
}

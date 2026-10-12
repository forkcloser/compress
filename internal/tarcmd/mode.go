package tarcmd

import "archive/tar"

// modeString renders a header's type and permission bits the way ls and
// tar -tv do: one type character, then rwx for user, group and other, with
// setuid, setgid and sticky folded into the execute positions.
func modeString(header *tar.Header) string {
	out := []byte("----------")

	switch header.Typeflag {
	case tar.TypeDir:
		out[0] = 'd'
	case tar.TypeSymlink:
		out[0] = 'l'
	case tar.TypeLink:
		out[0] = 'h'
	case tar.TypeChar:
		out[0] = 'c'
	case tar.TypeBlock:
		out[0] = 'b'
	case tar.TypeFifo:
		out[0] = 'p'
	}

	const (
		setuid = 0o4000
		setgid = 0o2000
		sticky = 0o1000
	)

	perm := header.Mode

	for i, bit := range []int64{0o400, 0o200, 0o100, 0o040, 0o020, 0o010, 0o004, 0o002, 0o001} {
		if perm&bit != 0 {
			out[1+i] = "rwxrwxrwx"[i]
		}
	}

	special := func(position int, bit int64, set, unset byte) {
		if perm&bit == 0 {
			return
		}

		if out[position] == 'x' {
			out[position] = set
		} else {
			out[position] = unset
		}
	}

	special(3, setuid, 's', 'S')
	special(6, setgid, 's', 'S')
	special(9, sticky, 't', 'T')

	return string(out)
}

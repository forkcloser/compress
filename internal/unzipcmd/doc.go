// Package unzipcmd is the unzip command: Info-ZIP's grammar and behavior
// for the subset of its options the rig's scripts use, on every platform,
// in pure Go.
//
// unzip [-options] archive[.zip] [member ...] [-x member ...] [-d dir]:
// extracts the archive, or the members named, under dir or the current
// directory, through an os.Root so that no entry, however named or linked,
// lands outside it. Options: -q, -o, -n, -d, -l, -t, -p, -j, -x, -h. Member
// patterns take "*" and "?", "*" matching across "/" as Info-ZIP's does.
// Anything else is a usage error. Without -o or -n, a file that exists is
// left alone and reported, where Info-ZIP would ask.
//
// Exit status is Info-ZIP's: 0, 1 after a warning (an entry skipped), 2 when
// an entry is damaged, 9 when the archive cannot be found or read, 10 for a
// usage error, 11 when no member matched. Diagnostics go to standard error.
package unzipcmd

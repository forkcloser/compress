// Package zipcmd is the zip command: Info-ZIP's grammar and behavior for the
// subset of its options the rig's scripts and curl-for-win use, on every
// platform, in pure Go.
//
// zip [-options] archive [input ...]: each input is stored under its path
// with the leading "./" and "/" removed, a directory as its own entry and,
// with -r, everything under it. An archive that exists is updated: an input
// replaces the entry of its name, the rest are kept. Options: -r, -q, -0
// through -9, -X, -@, -o, -j, -D, -h, -v. An archive named "-" is written
// to standard output. Symbolic links are followed, as Info-ZIP does without
// -y, except that -r does not walk into a link to a directory. A file that
// deflate does not shrink is deflated all the same, where
// Info-ZIP would store it. Anything else is a usage error.
//
// Exit status is Info-ZIP's: 0, 3 for an archive that is not a zip file, 12
// when there is nothing to do, 15 when the archive cannot be written, 16 for
// a usage error, 18 when an input could not be read. Diagnostics go to
// standard error in Info-ZIP's words.
package zipcmd

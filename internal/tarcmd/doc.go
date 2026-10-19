// Package tarcmd is the tar command: GNU tar's grammar and behavior for the
// subset of its options the rig's scripts, curl-for-win and actions/cache
// use, on every platform, in pure Go.
//
// Modes: --create (-c), --extract (-x, --get) and --list (-t). Options: -f,
// -C, -v, -z, -J, -j, --zstd, --lz4, -a, -p, -m, -T, --strip-components,
// --no-same-owner, --no-same-permissions, --format, --owner, --group,
// --numeric-owner, --mtime, --sort, --no-recursion, --exclude, -P
// (--absolute-names), -I (--use-compress-program), --posix,
// --delay-directory-restore and --force-local (accepted: directory
// metadata is always restored last, and there are no remote archives),
// --help, --version. The
// first argument may be an old-style option bundle (xf, cvzf); short options
// cluster (-xzf); long options take their argument after '=' or as the next
// word and may be given as any unambiguous prefix of their name, as GNU tar
// and bsdtar take them. Anything else is a usage error. Members named for --extract or
// --list, on the command line or in -T files, select the entries taken: a
// name as the archive carries it, or a directory with everything under it;
// one no entry carries is reported and the status is 2.
//
// Exit status is GNU's: 0, 2 after any error (a fatal one stops the run, a
// per-member one is reported and the run goes on), 64 for a usage error.
// Diagnostics go to standard error prefixed "tar: ".
package tarcmd

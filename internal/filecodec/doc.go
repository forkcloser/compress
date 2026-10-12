// Package filecodec is the engine behind the xz, gzip and zstd commands:
// the file-by-file behavior they share, as the tools of those names behave.
//
// With no file, or "-", the command filters standard input to standard
// output. With files, each is compressed to its name plus the format's
// suffix, or decompressed to its name without it, the result taking the
// original's mode and modification time, and the original is removed
// unless --keep or --stdout (zstd keeps it unless --rm). An existing result
// is not overwritten without --force; compressed data is not written to a
// terminal without --force.
//
// Exit status is the tools': 0, 1 after an error, and after a warning alone
// (a file skipped for its suffix, an existing result left alone) the tool's
// own: 2 for xz and gzip, 1 for zstd. Diagnostics go to standard error
// prefixed with the tool's name.
package filecodec

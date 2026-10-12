# compress

Compression and archiving for the toolchain, in pure Go: a library for
streaming zstd, gzip, xz, lz4 and bzip2, and the `tar`, `xz`, `gzip`, `zstd`,
`zip` and `unzip` commands built on it, released for every platform the rig
runs on and pinned through aqua like every other tool. Exists because the
rig's builds took `tar`, `xz`, `gzip` and `unzip` from whatever the host had:
GNU tar on Linux, bsdtar on macOS, Homebrew's on a runner, none of it pinned.
One module, one set of behaviors, verified releases.

## The library

The library is primordium's `compress` package, moved here so it can ship
binaries. zstd is built in; the other formats register themselves on import:

```go
import (
    "github.com/forkcloser/compress"
    _ "github.com/forkcloser/compress/gzip"
    _ "github.com/forkcloser/compress/xz"
)

reader, err := compress.Decompress(stream) // the format from its magic bytes
```

`Compress` is the zstd encoder, `Decompress` picks the decoder by magic
(`ErrUnknownFormat` for anything unregistered, `io.ErrUnexpectedEOF` for an
empty stream), `ReadAhead` runs a serial decoder a few blocks ahead of a slow
consumer, and `Tar`/`Untar` archive and extract a tree of regular files and
directories, refusing an entry that would land outside the destination.

## The commands

Each command keeps the grammar, output and exit statuses of the tool it
stands in for, for the subset of options the rig's scripts and curl-for-win
use, and refuses anything else as a usage error rather than guessing.

- **tar**: `-c`, `-x`, `-t`; `-f`, `-C`, `-v`, `-T`, `-p`, `-m`; `-z`, `-J`,
  `-j` (reading only), `--zstd`, `--lz4`, `-a`; `--strip-components`,
  `--no-same-owner`, `--no-same-permissions`, `--format`, `--owner`, `--group`,
  `--numeric-owner`, `--mtime`, `--sort`, `--no-recursion`. The old-style
  first argument (`xf`, `cvzf`) and clustered short options work as in GNU
  tar; a read without a compression flag picks the decoder by magic. Extraction
  goes through `os.Root`, so no entry, however named or linked, lands outside
  the destination. Exit 0, 2 after any error, 64 for a usage error.
- **xz**, **gzip** and **zstd**: files compressed beside themselves,
  decompressed back by suffix, standard streams when no file is named;
  `-c`, `-d`, `-f`, `-k`, `-q`, `-t`, the levels (`-1`…`-9`, zstd's up to
  `-19`), gzip's `-n`/`-N`, zstd's `--rm`. xz and gzip remove their input
  unless `-k`; zstd keeps it unless `--rm`, as the tools do. `unxz`, `xzcat`,
  `gunzip`, `zcat`, `unzstd` and `zstdcat` are the same binaries invoked by
  those names. Exit 0, 1 after an error, 2 after a warning alone (zstd has
  no warning status: 1).
- **zip**: `zip [-options] archive [input ...]` with `-r`, `-q`, `-0`…`-9`,
  `-X`, `-@`, `-o`, `-j`, `-D`; an existing archive is updated, `-` writes
  to standard output, names come from standard input under `-@`, and `-X`
  leaves only the DOS time fields, which is how curl-for-win packs. Exit
  statuses are Info-ZIP's: 12 for nothing to do, 16 for a usage error, 18
  for an input that could not be read.
- **unzip**: `unzip [-options] archive[.zip] [member ...] [-x member ...]
  [-d dir]` with `-q`, `-o`, `-n`, `-l`, `-t`, `-p`, `-j`; patterns take `*`
  and `?`. Extraction goes through `os.Root` like tar's. Exit statuses are
  Info-ZIP's: 1 after a warning, 9 for a missing archive, 10 for a usage
  error, 11 when no member matched.

xz's compression ratio is forkcloser/xz's, below the C tool's at the same
preset; decoding is at parity.

## Releasing

Tag `vX.Y.Z`; the release workflow builds every binary for
darwin/arm64, linux/amd64, linux/arm64, windows/amd64 and windows/arm64, one
`tar.gz` archive per platform carrying them all, with a `checksums.txt`
signed keyless by the workflow. This is the reference entry for a consumer's
local aqua registry; `gtar` is the same `tar` under the name curl-for-win
calls on macOS:

```yaml
- type: github_release
  repo_owner: forkcloser
  repo_name: compress
  description: tar, xz, gzip, zstd, zip and unzip in pure Go (forkcloser/compress)
  asset: compress_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
  format: tar.gz
  files:
    - name: tar
    - name: gtar
      src: tar
    - name: xz
    - name: gzip
    - name: zstd
    - name: zip
    - name: unzip
  checksum:
    type: github_release
    asset: checksums.txt
    algorithm: sha256
    cosign:
      bundle:
        type: github_release
        asset: checksums.txt.sigstore.json
      opts:
        - --certificate-identity
        - https://github.com/forkcloser/compress/.github/workflows/release.yaml@refs/tags/{{.Version}}
        - --certificate-oidc-issuer
        - https://token.actions.githubusercontent.com
  supported_envs:
    - darwin/arm64
    - linux/amd64
    - linux/arm64
    - windows/amd64
    - windows/arm64
```

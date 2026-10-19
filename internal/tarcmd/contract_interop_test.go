package tarcmd_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/compress/internal/tarcmd"
)

// entry is what an extracted tree holds at a path: the kind, the
// permission bits, the content or link target, and the mtime in seconds.
type entry struct {
	kind    string
	mode    os.FileMode
	content string
	mtime   int64
}

func snapshot(t *testing.T, root string) map[string]entry {
	t.Helper()

	out := map[string]entry{}

	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}

		e := entry{mode: info.Mode().Perm(), mtime: info.ModTime().Unix()}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			e.kind = "symlink"
			e.content, _ = os.Readlink(path)
			e.mtime = 0
		case info.IsDir():
			e.kind = "dir"
		default:
			e.kind = "file"
			b, _ := os.ReadFile(path)
			e.content = string(b)
		}

		out[filepath.ToSlash(rel)] = e

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return out
}

// richTree is a tree with the shapes a tar format has to carry: a path
// over 100 bytes and one over 255, a non-ASCII name (of characters with
// no canonical decomposition, so no filesystem renormalizes it), a name
// with spaces, an
// empty file, an executable, a private file, a symlink, a hard link, an
// empty directory, and a fixed mtime on everything.
func richTree(t *testing.T) string {
	t.Helper()

	src := filepath.Join(t.TempDir(), "src")
	long := strings.Repeat("d", 60) + "/" + strings.Repeat("e", 60) + "/" + strings.Repeat("f", 40) + ".txt"
	veryLong := strings.Repeat("g", 90) + "/" + strings.Repeat("h", 90) + "/" + strings.Repeat("i", 90) + ".txt"

	for _, d := range []string{"sub", "empty", filepath.Dir(long), filepath.Dir(veryLong), "\u65e5\u672c\u8a9e dir"} {
		if err := os.MkdirAll(filepath.Join(src, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for path, c := range map[string]struct {
		content string
		mode    os.FileMode
	}{
		"hello.txt":            {"hello\n", 0o644},
		"sub/run.sh":           {"#!/bin/sh\n", 0o755},
		"sub/private":          {"secret", 0o600},
		"sub/empty.txt":        {"", 0o644},
		"name with spaces.txt": {"spaced", 0o644},
		"\u65e5\u672c\u8a9e dir/\u30d5\u30a1\u30a4\u30eb.txt": {"unicode", 0o644},
		long:      {"long", 0o644},
		veryLong:  {"very long", 0o644},
		"big.bin": {strings.Repeat("b", 100000), 0o644},
	} {
		full := filepath.Join(src, filepath.FromSlash(path))
		if err := os.WriteFile(full, []byte(c.content), c.mode); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(full, c.mode); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Symlink("hello.txt", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(filepath.Join(src, "hello.txt"), filepath.Join(src, "sub", "hard")); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2024, time.March, 4, 5, 6, 7, 0, time.UTC)

	_ = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode()&os.ModeSymlink == 0 {
			_ = os.Chtimes(path, at, at)
		}

		return nil
	})

	return src
}

// systemPath is PATH without the hermetic root: everywhere the suite runs,
// that root heads PATH and its `tar`, `gzip`, `xz` and `zstd` are links to
// compress's own release, and an oracle found through it would hold the
// package to its previous release. The host's own directories, the system's
// and a package manager's, stay.
func systemPath() string {
	var kept []string

	for dir := range strings.SplitSeq(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if dir == "" || strings.Contains(dir, "hermetic") || strings.Contains(dir, "aquaproj") ||
			strings.Contains(dir, "limen") {
			continue
		}

		kept = append(kept, dir)
	}

	return strings.Join(
		append(kept, "/usr/bin", "/bin", "/usr/local/bin", "/opt/homebrew/bin"),
		string(os.PathListSeparator),
	)
}

// systemTool finds name among the host's own tools, never the hermetic
// root's links.
func systemTool(name string) (string, bool) {
	for dir := range strings.SplitSeq(systemPath(), string(os.PathListSeparator)) {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, true
		}
	}

	return "", false
}

func systemTar(t *testing.T) string {
	t.Helper()

	for _, name := range []string{"bsdtar", "gtar", "tar"} {
		if path, ok := systemTool(name); ok {
			return path
		}
	}

	t.Skip("no system tar outside the hermetic root")

	return ""
}

func mine(t *testing.T, args ...string) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	if status := tarcmd.Run(args, nil, &stdout, &stderr, "test"); status != 0 {
		t.Fatalf("tar %s: exit %d\n%s", strings.Join(args, " "), status, stderr.String())
	}
}

func theirs(t *testing.T, tar string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), tar, args...)
	// macOS's bsdtar would add AppleDouble "._" entries for extended
	// attributes; the trees here have none worth carrying. A UTF-8 locale,
	// or bsdtar's listing escapes every non-ASCII byte as octal. The system
	// PATH, so a GNU tar runs the host's gzip, xz and zstd, not ours.
	cmd.Env = append(os.Environ(), "COPYFILE_DISABLE=1", "LC_ALL=en_US.UTF-8", "PATH="+systemPath())

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", tar, strings.Join(args, " "), err, out)
	}

	return string(out)
}

// TestContractInteropRichTree: an archive this tar creates extracts under
// the system tar to the same tree (kinds, permission bits, contents,
// link targets, mtimes), and an archive the system tar creates extracts
// under this tar to the same tree; both list the same names.
func TestContractInteropRichTree(t *testing.T) {
	t.Parallel()

	tar := systemTar(t)
	src := richTree(t)
	want := snapshot(t, src)

	// Hard links: an archive may carry the second name as a link or a copy;
	// extracted, both read the same bytes, which is what the snapshot holds.
	for _, direction := range []string{"mine-to-theirs", "theirs-to-mine"} {
		t.Run(direction, func(t *testing.T) {
			t.Parallel()

			work := t.TempDir()
			archive := filepath.Join(work, "a.tar")
			dst := filepath.Join(work, "dst")

			if err := os.Mkdir(dst, 0o755); err != nil {
				t.Fatal(err)
			}

			var listing string

			if direction == "mine-to-theirs" {
				mine(t, "-cf", archive, "-C", src, ".")
				theirs(t, tar, "-xpf", archive, "-C", dst)
				listing = theirs(t, tar, "-tf", archive)
			} else {
				theirs(t, tar, "-cf", archive, "-C", src, ".")
				mine(t, "-xpf", archive, "-C", dst)

				var out, errOut bytes.Buffer
				if status := tarcmd.Run([]string{"-tf", archive}, nil, &out, &errOut, "test"); status != 0 {
					t.Fatalf("-tf: exit %d %s", status, errOut.String())
				}

				listing = out.String()
			}

			got := snapshot(t, dst)

			for path, w := range want {
				g, ok := got[path]
				if !ok {
					t.Errorf("%s: missing after extraction", path)

					continue
				}

				if g.kind != w.kind || g.content != w.content ||
					(w.kind != "symlink" && (g.mode != w.mode || g.mtime != w.mtime)) {
					t.Errorf("%s: %+v, want %+v", path, g, w)
				}
			}

			for path := range got {
				if _, ok := want[path]; !ok {
					t.Errorf("%s: extracted, not in the source", path)
				}
			}

			var names []string

			for line := range strings.SplitSeq(strings.TrimSpace(listing), "\n") {
				name := strings.TrimSuffix(strings.TrimPrefix(line, "./"), "/")
				if name != "" && name != "." {
					names = append(names, name)
				}
			}

			slices.Sort(names)

			wantNames := make([]string, 0, len(want))
			for path := range want {
				wantNames = append(wantNames, path)
			}

			slices.Sort(wantNames)

			if !slices.Equal(names, wantNames) {
				t.Errorf("listing %v\nwant %v", names, wantNames)
			}
		})
	}
}

// TestContractInteropCompressed: the three compressed forms this tar
// writes (-z, -J, --zstd) open under the system tar with the matching
// flag, and the system tar's open under this tar's magic sniffing with
// no flag at all.
func TestContractInteropCompressed(t *testing.T) {
	t.Parallel()

	tar := systemTar(t)

	for _, c := range []struct {
		flag string
		tool string
	}{{"-z", "gzip"}, {"-J", "xz"}, {"--zstd", "zstd"}} {
		t.Run(c.flag, func(t *testing.T) {
			t.Parallel()

			if _, ok := systemTool(c.tool); !ok {
				t.Skipf("no %s for the system tar", c.tool)
			}

			src := richTree(t)
			want := snapshot(t, src)
			work := t.TempDir()
			archive := filepath.Join(work, "a.tar.c")

			mine(t, "-cf", archive, c.flag, "-C", src, ".")

			dst := filepath.Join(work, "theirs")
			if err := os.Mkdir(dst, 0o755); err != nil {
				t.Fatal(err)
			}

			theirs(t, tar, "-xpf", archive, c.flag, "-C", dst)

			if got := snapshot(t, dst); len(got) != len(want) {
				t.Errorf(
					"the system tar extracted %d entries from this tar's %s archive, want %d",
					len(got),
					c.flag,
					len(want),
				)
			}

			back := filepath.Join(work, "b.tar.c")
			theirs(t, tar, "-cf", back, c.flag, "-C", src, ".")

			dst = filepath.Join(work, "mine")
			if err := os.Mkdir(dst, 0o755); err != nil {
				t.Fatal(err)
			}

			mine(t, "-xpf", back, "-C", dst)

			if got := snapshot(t, dst); len(got) != len(want) {
				t.Errorf(
					"this tar extracted %d entries from the system tar's %s archive by magic, want %d",
					len(got),
					c.flag,
					len(want),
				)
			}
		})
	}
}

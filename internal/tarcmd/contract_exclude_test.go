package tarcmd_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/reference"
)

// gnuTar is the GNU tar on PATH, the tool --exclude is held to; the test is
// skipped where there is none.
func gnuTar(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the GNU tar on a Windows runner's PATH is Git for Windows', which fails on these paths")
	}

	system := reference.Find(t, "tar", "--version")

	version, err := exec.CommandContext(t.Context(), system, "--version").Output()
	if err != nil || !strings.Contains(string(version), "GNU tar") {
		t.Skip("the tar on PATH is not GNU tar")
	}

	return system
}

// gnu runs GNU tar and returns its standard output; its diagnostics are
// shown when it fails.
func gnu(t *testing.T, tar string, args ...string) []byte {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), tar, args...)
	// A UTF-8 locale: a character is then one for GNU tar's "?" and classes,
	// as it is here, and not each of its bytes.
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")

	out, err := cmd.Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			t.Fatalf("GNU tar %s: %v\n%s", strings.Join(args, " "), err, exit.Stderr)
		}

		t.Fatalf("GNU tar %s: %v", strings.Join(args, " "), err)
	}

	return out
}

// excludeTree is the tree the patterns are tried on: a name that is a
// directory at the top and under another, a file of a directory's name, and
// names three levels down.
func excludeTree(t *testing.T) string {
	t.Helper()

	root := filepath.Join(t.TempDir(), "root")

	for _, file := range []string{
		"dir/a", "dir/sub/b", "dir2/x", "other/dir/c", "etc/passwd", "a/b/c.txt", "file.bin",
	} {
		path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		writeFile(t, path, file, 0o644)
	}

	return root
}

// oddTree is names that tell a class from its characters: a hyphen, an
// underscore, a digit, brackets, and a character of two bytes.
func oddTree(t *testing.T) string {
	t.Helper()

	root := filepath.Join(t.TempDir(), "odd")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, file := range oddNames() {
		writeFile(t, filepath.Join(root, file), file, 0o644)
	}

	return root
}

func oddNames() []string {
	return []string{"-", "[!x]", "a[b", "ab", "café", "file1", "filea", "x-old", "x_old", "xold"}
}

// oddPatterns are patterns over oddTree's names, each with the names it
// leaves out.
func oddPatterns() map[string][]string {
	return map[string][]string{
		"*[-_]old":         {"x-old", "x_old"},
		"x[_-]old":         {"x-old", "x_old"},
		"x[^-_]ld":         {"xold"},
		"[x-]*":            {"-", "x-old", "x_old", "xold"},
		"x[!-]old":         {"x_old"},
		"x[--a]old":        {"x-old", "x_old"},
		"file[[:digit:]]":  {"file1"},
		"file[![:digit:]]": {"filea"},
		"[[:alpha:]]b":     {"ab"},
		"[[:alpha:][]*b":   {"a[b", "ab"},
		"x[[:punct:]]old":  {"x-old", "x_old"},
		"[[:nothing:]]b":   nil,
		"file[![:foo:]]":   nil,
		"[[:foo:]a]b":      nil,
		"[a[:foo:]]b":      {"ab"},
		"a[b":              {"a[b"},
		"a[*":              {"a[b"},
		`\[!x]`:            {"[!x]"},
		"[!x]b":            {"ab"},
		`[\!x]*`:           {"x-old", "x_old", "xold"},
		"caf?":             {"café"},
		"caf[!e]":          {"café"},
		"c*é":              {"café"},
		`ab\`:              nil,
	}
}

func sorted(lines []string) []string {
	out := slices.Clone(lines)
	slices.Sort(out)

	return out
}

func lines(out []byte) []string {
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' })
}

// TestContractInteropExclude: --exclude leaves out, on create, on list and
// on extract, the names GNU tar leaves out for the same pattern.
func TestContractInteropExclude(t *testing.T) {
	t.Parallel()

	tar := gnuTar(t)
	root := excludeTree(t)
	rootless := strings.TrimPrefix(filepath.ToSlash(root), "/")

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		for _, args := range [][]string{
			{"-C", root, "--exclude", "dir", "."},
			{"-C", root, "--exclude", "dir/", "."},
			{"-C", root, "--exclude", "/dir", "."},
			{"-C", root, "--exclude", "./dir", "."},
			{"-C", root, "--exclude", "dir/*", "."},
			{"-C", root, "--exclude", "dir*", "."},
			{"-C", root, "--exclude", "*/c", "."},
			{"-C", root, "--exclude", "sub/b", "."},
			{"-C", root, "--exclude", "b/c.txt", "."},
			{"-C", root, "--exclude", "*.txt", "."},
			{"-C", root, "--exclude", "di?", "."},
			{"-C", root, "--exclude", "[d]ir", "."},
			{"-C", root, "--exclude", "a", "."},
			{"-C", root, "--exclude", "file.bin", "."},
			{"-C", root, "--exclude", "a*c.txt", "."},
			{"-C", root, "--exclude", "d*b", "."},
			{"-C", root, "--exclude", "dir?a", "."},
			{"-C", root, "--exclude", "other*c", "."},
			{"-C", root, "--exclude", "[", "."},
			{"-C", root, "--exclude", "[!x]ir", "."},
			{"-C", root, "--exclude", "[^x]ir", "."},
			{"-C", root, "--exclude", "[!d]ir", "."},
			{"-C", root, "--exclude", "[]d]ir", "."},
			{"-C", root, "--exclude", "[!]x]ir", "."},
			{"-C", root, "--exclude", `d\ir`, "."},
			{"-C", root, "--exclude", `di[q-s]`, "."},
			{"-C", root, "--exclude", "other/dir", "."},
			{"-C", root, "--exclude", "ETC", "."},
			{"-C", root, "--exclude", ".", "."},
			{"-C", root, "--exclude", "etc", "etc", "dir"},
			{"-C", root, "--exclude", "etc", "./etc", "dir"},
			{"-C", root, "--exclude", "./etc", "etc", "dir"},
			{"-C", root, "--exclude", "*", "etc"},
			{"-C", root, "--no-recursion", "--exclude", "dir", "dir", "dir/a", "other/dir/c", "etc"},
			{"-C", root, "--exclude", "dir", "--no-recursion", "dir", "dir/a", "other/dir/c", "etc"},
			{"--exclude", root + "/dir", root},
			{"--exclude", rootless + "/dir", root},
			{"--exclude", "dir", root},
			{"-P", "--exclude", root + "/dir", root},
			{"-P", "--exclude", root + "/dir/", root},
			{"-P", "--exclude", rootless + "/dir", root},
			{"-P", "--exclude", "dir", root},
			{"-P", "--exclude", "/dir", root},
		} {
			want := sorted(names(t, gnu(t, tar, append([]string{"-cf", "-"}, args...)...)))

			status, archive, diag := run(t, nil, append([]string{"-cf", "-"}, args...)...)
			if status != 0 {
				t.Errorf("tar -cf - %s: exit %d\n%s", strings.Join(args, " "), status, diag)

				continue
			}

			if got := sorted(names(t, []byte(archive))); !slices.Equal(got, want) {
				t.Errorf("tar -cf - %s:\n ours %q\n GNU  %q", strings.Join(args, " "), got, want)
			}
		}
	})

	t.Run("classes", func(t *testing.T) {
		t.Parallel()

		odd := oddTree(t)

		for pattern := range oddPatterns() {
			args := []string{"-cf", "-", "-C", odd, "--exclude", pattern, "."}
			want := sorted(names(t, gnu(t, tar, args...)))

			archive, _ := mustRun(t, nil, args...)
			if got := sorted(names(t, []byte(archive))); !slices.Equal(got, want) {
				t.Errorf("--exclude %s:\n ours %q\n GNU  %q", pattern, got, want)
			}
		}
	})

	patterns := []string{
		"dir", "dir/", "/dir", "./dir", "dir/*", "dir*", "*/c", "sub/b", "b/c.txt", "*.txt", "di?", "a",
		"a*c.txt", "d*b", "dir?a", "other*c", "[", "[!x]ir", "[^x]ir", "[!d]ir", "[]d]ir", "[!]x]ir", `d\ir`, "di[q-s]",
		"other/dir", ".", "etc", "./etc", "*", root + "/dir", root + "/dir/", rootless + "/dir",
	}

	for shape, create := range map[string][]string{
		"names under dot": {"-C", root, "."},
		"plain names":     {"-C", root, "dir", "dir2", "other", "etc", "a", "file.bin"},
		"absolute names":  {"-P", root},
	} {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()

			archive := filepath.Join(t.TempDir(), "a.tar")
			mustRun(t, nil, append([]string{"-cf", archive}, create...)...)

			for _, pattern := range patterns {
				want := sorted(lines(gnu(t, tar, "-tf", archive, "--exclude", pattern)))

				status, out, diag := run(t, nil, "-tf", archive, "--exclude", pattern)
				if got := sorted(lines([]byte(out))); status != 0 || !slices.Equal(got, want) {
					t.Errorf("tar -t --exclude %s: exit %d %s\n ours %q\n GNU  %q", pattern, status, diag, got, want)
				}

				if shape == "absolute names" {
					continue
				}

				theirs, ours := t.TempDir(), t.TempDir()
				gnu(t, tar, "-xf", archive, "-C", theirs, "--exclude", pattern)

				if status, _, diag := run(t, nil, "-xf", archive, "-C", ours, "--exclude", pattern); status != 0 {
					t.Errorf("tar -x --exclude %s: exit %d\n%s", pattern, status, diag)

					continue
				}

				if got, want := found(t, ours), found(t, theirs); !slices.Equal(got, want) {
					t.Errorf("tar -x --exclude %s:\n ours %q\n GNU  %q", pattern, got, want)
				}
			}
		})
	}
}

// found is the names under root, sorted.
func found(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	for name := range snapshot(t, root) {
		out = append(out, name)
	}

	slices.Sort(out)

	return out
}

// TestContractExclude: what --exclude leaves out, where there is no GNU tar
// to ask: the answers are the ones TestContractInteropExclude got from it.
// A pattern is tried on the whole name, on every tail after a slash and on
// the leading directories of each; its wildcards and classes match a
// slash; "!" and "^" negate a class, which takes ranges, the classes POSIX
// names, a "]" that comes first and a "-" that has no character on both
// sides; a bracket that opens no class is itself and a backslash takes the
// next character as itself; a slash at the pattern's end matches nothing,
// and one at its start matches a name that has one. The listing leaves out
// what extraction does.
func TestContractExclude(t *testing.T) {
	t.Parallel()

	root := excludeTree(t)
	all := []string{
		"./", "./a/", "./a/b/", "./a/b/c.txt", "./dir/", "./dir/a", "./dir/sub/", "./dir/sub/b", "./dir2/",
		"./dir2/x", "./etc/", "./etc/passwd", "./file.bin", "./other/", "./other/dir/", "./other/dir/c",
	}

	without := func(dropped ...string) []string {
		return slices.DeleteFunc(slices.Clone(all), func(name string) bool {
			return slices.ContainsFunc(dropped, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
		})
	}

	archive := filepath.Join(t.TempDir(), "a.tar")
	mustRun(t, nil, "-cf", archive, "-C", root, ".")

	for pattern, want := range map[string][]string{
		"dir":      without("./dir/", "./other/dir/"),
		"./dir":    without("./dir/"),
		"dir/":     all,
		"/dir":     all,
		"dir/*":    without("./dir/a", "./dir/sub/", "./other/dir/c"),
		"a*c.txt":  without("./a/b/c.txt"),
		"dir?a":    without("./dir/a"),
		"other*c":  without("./other/dir/c"),
		"[!x]ir":   without("./dir/", "./other/dir/"),
		"[^x]ir":   without("./dir/", "./other/dir/"),
		"[!d]ir":   all,
		"[]d]ir":   without("./dir/", "./other/dir/"),
		"di[q-s]":  without("./dir/", "./other/dir/"),
		`d\ir`:     without("./dir/", "./other/dir/"),
		"[":        all,
		".":        nil,
		"*":        nil,
		"file.bin": without("./file.bin"),
	} {
		created, _ := mustRun(t, nil, "-cf", "-", "-C", root, "--exclude", pattern, ".")
		if got := sorted(names(t, []byte(created))); !slices.Equal(got, want) {
			t.Errorf("-c --exclude %s:\n got  %q\n want %q", pattern, got, want)
		}

		listed, _ := mustRun(t, nil, "-tf", archive, "--exclude", pattern)
		if got := sorted(lines([]byte(listed))); !slices.Equal(got, want) {
			t.Errorf("-t --exclude %s:\n got  %q\n want %q", pattern, got, want)
		}
	}

	odd := oddTree(t)

	for pattern, dropped := range oddPatterns() {
		want := []string{"./"}

		for _, name := range oddNames() {
			if !slices.Contains(dropped, name) {
				want = append(want, "./"+name)
			}
		}

		created, _ := mustRun(t, nil, "-cf", "-", "-C", odd, "--exclude", pattern, ".")
		if got := sorted(names(t, []byte(created))); !slices.Equal(got, sorted(want)) {
			t.Errorf("-c --exclude %s:\n got  %q\n want %q", pattern, got, sorted(want))
		}
	}

	// An exclusion given after --no-recursion matches a name and no longer
	// what is under it; given before, it covers both.
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"--no-recursion", "--exclude", "dir"}, []string{"dir/a", "etc/", "other/dir/c"}},
		{[]string{"--exclude", "dir", "--no-recursion"}, []string{"etc/"}},
	} {
		args := append(append([]string{"-cf", "-", "-C", root}, tc.args...), "dir", "dir/a", "other/dir/c", "etc")

		created, _ := mustRun(t, nil, args...)
		if got := sorted(names(t, []byte(created))); !slices.Equal(got, tc.want) {
			t.Errorf("tar %s:\n got  %q\n want %q", strings.Join(args, " "), got, tc.want)
		}
	}

	// A member named by an absolute path is matched as it was named, its
	// leading slash still there, whether or not -P keeps it in the archive.
	slashed := filepath.ToSlash(root)
	if !strings.HasPrefix(slashed, "/") {
		return // a drive letter: no leading slash to keep
	}

	for _, args := range [][]string{
		{"--exclude", slashed + "/dir", root},
		{"--exclude", strings.TrimPrefix(slashed, "/") + "/dir", root},
		{"-P", "--exclude", slashed + "/dir", root},
		{"-P", "--exclude", strings.TrimPrefix(slashed, "/") + "/dir", root},
	} {
		created, _ := mustRun(t, nil, append([]string{"-cf", "-"}, args...)...)

		got := names(t, []byte(created))
		if slices.ContainsFunc(got, func(name string) bool { return strings.HasSuffix(name, "/root/dir/a") }) ||
			!slices.ContainsFunc(got, func(name string) bool { return strings.HasSuffix(name, "/root/other/dir/c") }) {
			t.Errorf("-c %s: %q", strings.Join(args, " "), got)
		}
	}
}

package xzcmd_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/filecodec"
)

// TestContractForceTakesTheNameNotTheFile: --force replaces what holds the
// counterpart's name. A symbolic link there is replaced, not written
// through, and a file with other names keeps its content under them, in
// both directions; the counterpart is a new regular file. A link to
// nothing and a link to a directory are replaced as any link is, and so
// is a file that cannot be written. A directory at the name is not removed, and the run fails; so does it,
// with the reason, when what holds the name cannot be removed.
func TestContractForceTakesTheNameNotTheFile(t *testing.T) {
	t.Parallel()

	const precious = "precious\n"

	for name, link := range map[string]func(victim, at string) error{
		"a symbolic link": func(_, at string) error { return os.Symlink("victim", at) },
		"a hard link":     os.Link,
	} {
		for direction, args := range map[string][]string{"compressing": {"-kf"}, "decompressing": {"-dkf"}} {
			t.Run(name+", "+direction, func(t *testing.T) {
				t.Parallel()

				dir := t.TempDir()
				plain, packed, victim := filepath.Join(dir, "data"), filepath.Join(dir, "data.xz"),
					filepath.Join(dir, "victim")

				writeFile(t, plain, payload)
				mustRun(t, "xz", nil, "-k", plain)
				writeFile(t, victim, precious)

				// The input stays; the other name is the one in the way.
				input, target := plain, packed
				if direction == "decompressing" {
					input, target = packed, plain
				}

				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}

				if err := link(victim, target); err != nil {
					t.Skipf("no such link here: %v", err)
				}

				mustRun(t, "xz", nil, append(args, input)...)

				if got := readFile(t, victim); got != precious {
					t.Errorf("the file behind the name was written: %d bytes", len(got))
				}

				if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() {
					t.Fatalf("the counterpart: %v", err)
				}

				want := payload
				if direction == "compressing" {
					want = decode(t, []byte(readFile(t, target)))
				}

				if got := readFile(t, plain); direction == "decompressing" && got != want {
					t.Errorf("decompressed %q", got)
				}

				if direction == "compressing" && want != payload {
					t.Errorf("the counterpart decodes to %q", want)
				}
			})
		}
	}

	t.Run("a link to nothing, a link to a directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		plain := filepath.Join(dir, "data")
		writeFile(t, plain, payload)

		if err := os.Mkdir(filepath.Join(dir, "elsewhere"), 0o755); err != nil {
			t.Fatal(err)
		}

		for _, to := range []string{"nothing", "elsewhere"} {
			_ = os.Remove(plain + ".xz")

			if err := os.Symlink(to, plain+".xz"); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}

			mustRun(t, "xz", nil, "-kf", plain)

			if info, err := os.Lstat(plain + ".xz"); err != nil || !info.Mode().IsRegular() {
				t.Errorf("over a link to %s, the counterpart: %v", to, err)
			}
		}

		if _, err := os.Lstat(filepath.Join(dir, "nothing")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the link's target was made: %v", err)
		}

		if entries, err := os.ReadDir(filepath.Join(dir, "elsewhere")); err != nil || len(entries) != 0 {
			t.Errorf("the directory behind the link: %v, %v", entries, err)
		}
	})

	t.Run("a file that cannot be written", func(t *testing.T) {
		t.Parallel()

		plain := filepath.Join(t.TempDir(), "data")
		writeFile(t, plain, payload)
		writeFile(t, plain+".xz", "in the way")

		if err := os.Chmod(plain+".xz", 0o400); err != nil {
			t.Fatal(err)
		}

		mustRun(t, "xz", nil, "-kf", plain)

		if got := decode(t, []byte(readFile(t, plain+".xz"))); got != payload {
			t.Errorf("the counterpart decodes to %q", got)
		}
	})

	t.Run("a name that cannot be removed", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a directory this user cannot write to")
		}

		dir := filepath.Join(t.TempDir(), "closed")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		plain := filepath.Join(dir, "data")
		writeFile(t, plain, payload)
		writeFile(t, plain+".xz", "in the way")

		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		// The reason is the removal's, not "already exists".
		status, _, stderr := run(t, "xz", nil, "-kf", plain)
		if status != filecodec.ExitError || !strings.Contains(stderr, "data.xz: permission denied") {
			t.Errorf("exit %d, stderr %q", status, stderr)
		}

		if got := readFile(t, plain+".xz"); got != "in the way" {
			t.Errorf("the file at the name: %q", got)
		}
	})

	t.Run("a directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		plain := filepath.Join(dir, "data")
		writeFile(t, plain, payload)

		if err := os.Mkdir(plain+".xz", 0o755); err != nil {
			t.Fatal(err)
		}

		status, _, stderr := run(t, "xz", nil, "-kf", plain)
		if status != filecodec.ExitError || !strings.Contains(stderr, "data.xz: is a directory") {
			t.Errorf("exit %d, stderr %q", status, stderr)
		}

		if info, err := os.Stat(plain + ".xz"); err != nil || !info.IsDir() {
			t.Errorf("the directory: %v", err)
		}
	})
}

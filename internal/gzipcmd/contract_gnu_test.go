package gzipcmd_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	kgzip "github.com/klauspost/compress/gzip"

	"github.com/forkcloser/go-compress/internal/reference"
)

// stream is a gzip stream of content whose header records name, when it is
// given one, and a time.
func stream(t *testing.T, content, name string) []byte {
	t.Helper()

	var buf bytes.Buffer

	writer := kgzip.NewWriter(&buf)
	writer.Name = name
	writer.ModTime = time.Date(2020, time.February, 3, 4, 5, 6, 0, time.UTC)

	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// holding is what a directory holds after a run: each name with its
// content, a gzip stream by what it decodes to, a link by its target.
func holding(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var out []string

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			out = append(out, entry.Name()+" -> "+target)
		case entry.IsDir():
			out = append(out, entry.Name()+"/")
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if reader, err := kgzip.NewReader(bytes.NewReader(data)); err == nil {
				if plain, err := io.ReadAll(reader); err == nil {
					data = append([]byte("gzip of "), plain...)
				}
			}

			out = append(out, entry.Name()+" = "+string(data))
		}
	}

	slices.Sort(out)

	return out
}

// TestContractInteropGNUGzip: over the same files and the same arguments,
// this gzip exits with GNU gzip's status and leaves what it leaves.
func TestContractInteropGNUGzip(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("no GNU gzip to compare with")
	}

	system := reference.Find(t, "gzip", "-V")

	version, err := exec.CommandContext(t.Context(), system, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), "Free Software Foundation") {
		t.Skip("the gzip on PATH is not GNU gzip")
	}

	plain := func(dir string) { writeFile(t, filepath.Join(dir, "f"), payload) }
	packed := func(name string, more ...byte) func(dir string) {
		return func(dir string) {
			data := append(stream(t, payload, ""), more...)
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
				t.Fatal(err)
			}
		}
	}

	type scenario struct {
		setup func(dir string)
		args  []string
	}

	scenarios := map[string]scenario{
		"compress":       {plain, []string{"f"}},
		"compress, keep": {plain, []string{"-k", "f"}},
		"level 19":       {plain, []string{"-19", "f"}},
		"-z":             {plain, []string{"-z", "f"}},
		"--dec":          {packed("f.gz"), []string{"--dec", "f.gz"}},
		"missing":        {plain, []string{"nothing"}},
		"a directory": {
			func(dir string) { _ = os.Mkdir(filepath.Join(dir, "d"), 0o755) },
			[]string{"d"},
		},
		"decompress":                          {packed("f.gz"), []string{"-d", "f.gz"}},
		"decompress, keep":                    {packed("f.gz"), []string{"-dk", "f.gz"}},
		"test":                                {packed("f.gz"), []string{"-t", "f.gz"}},
		"trailing garbage":                    {packed("f.gz", []byte("garbage")...), []string{"-d", "f.gz"}},
		"trailing garbage, test":              {packed("f.gz", []byte("garbage")...), []string{"-t", "f.gz"}},
		"trailing zeros":                      {packed("f.gz", make([]byte, 512)...), []string{"-d", "f.gz"}},
		"one trailing zero":                   {packed("f.gz", 0), []string{"-d", "f.gz"}},
		"zeros, then garbage":                 {packed("f.gz", 0, 0, 'x'), []string{"-d", "f.gz"}},
		"a member's first byte, then garbage": {packed("f.gz", 0x1f, 'j', 'u', 'n', 'k'), []string{"-d", "f.gz"}},
		"a member's first byte alone":         {packed("f.gz", 0x1f), []string{"-d", "f.gz"}},
		"zeros, then a member": {
			packed("f.gz", append([]byte{0, 0}, stream(t, "more", "")...)...),
			[]string{"-d", "f.gz"},
		},
		"suffix -z":              {packed("a-z"), []string{"-d", "a-z"}},
		"suffix _Z":              {packed("a_Z"), []string{"-d", "a_Z"}},
		"suffix .tAz":            {packed("a.tAz"), []string{"-d", "a.tAz"}},
		"already suffixed, .tgz": {packed("a.tgz"), []string{"a.tgz"}},
		"already suffixed, -q":   {packed("f.gz"), []string{"-q", "f.gz"}},
		"a symbolic link, -d": {
			func(dir string) { packed("f.gz")(dir); _ = os.Symlink("f.gz", filepath.Join(dir, "s.gz")) },
			[]string{"-d", "s.gz"},
		},
		"a symbolic link, -t": {
			func(dir string) { packed("f.gz")(dir); _ = os.Symlink("f.gz", filepath.Join(dir, "s.gz")) },
			[]string{"-t", "s.gz"},
		},
		"a symbolic link, -k": {
			func(dir string) { plain(dir); _ = os.Symlink("f", filepath.Join(dir, "s")) },
			[]string{"-k", "s"},
		},
		"another hard link, -k": {
			func(dir string) { plain(dir); _ = os.Link(filepath.Join(dir, "f"), filepath.Join(dir, "h")) },
			[]string{"-k", "f"},
		},
		"another hard link, -c": {
			func(dir string) { plain(dir); _ = os.Link(filepath.Join(dir, "f"), filepath.Join(dir, "h")) },
			[]string{"-c", "f"},
		},
		"two members": {packed("f.gz", stream(t, "more", "")...), []string{"-d", "f.gz"}},
		"cut short": {
			func(dir string) { packed("f.gz")(dir); _ = os.Truncate(filepath.Join(dir, "f.gz"), 20) },
			[]string{"-d", "f.gz"},
		},
		"empty": {
			func(dir string) { writeFile(t, filepath.Join(dir, "f.gz"), "") },
			[]string{"-d", "f.gz"},
		},
		"not gzip": {
			func(dir string) { writeFile(t, filepath.Join(dir, "f.gz"), "plain text") },
			[]string{"-d", "f.gz"},
		},
		"suffix .GZ":             {packed("F.GZ"), []string{"-d", "F.GZ"}},
		"suffix .tgz":            {packed("a.tgz"), []string{"-d", "a.tgz"}},
		"suffix .TGZ":            {packed("a.TGZ"), []string{"-d", "a.TGZ"}},
		"suffix .taz":            {packed("a.taz"), []string{"-d", "a.taz"}},
		"suffix -gz":             {packed("a-gz"), []string{"-d", "a-gz"}},
		"suffix _z":              {packed("a_z"), []string{"-d", "a_z"}},
		"suffix .z":              {packed("a.z"), []string{"-d", "a.z"}},
		"no suffix":              {packed("f.txt"), []string{"-d", "f.txt"}},
		"the suffix alone":       {packed(".gz"), []string{"-d", ".gz"}},
		"already suffixed":       {packed("f.gz"), []string{"f.gz"}},
		"counterpart exists":     {func(dir string) { plain(dir); packed("f.gz")(dir) }, []string{"f"}},
		"counterpart exists, -f": {func(dir string) { plain(dir); packed("f.gz")(dir) }, []string{"-f", "f"}},
		"decompressed exists":    {func(dir string) { plain(dir); packed("f.gz")(dir) }, []string{"-d", "f.gz"}},
		"a symbolic link": {
			func(dir string) { plain(dir); _ = os.Symlink("f", filepath.Join(dir, "s")) },
			[]string{"s"},
		},
		"a symbolic link, -f": {
			func(dir string) { plain(dir); _ = os.Symlink("f", filepath.Join(dir, "s")) },
			[]string{"-f", "s"},
		},
		"a symbolic link, -c": {
			func(dir string) { plain(dir); _ = os.Symlink("f", filepath.Join(dir, "s")) },
			[]string{"-c", "s"},
		},
		"another hard link": {
			func(dir string) { plain(dir); _ = os.Link(filepath.Join(dir, "f"), filepath.Join(dir, "h")) },
			[]string{"f"},
		},
		"another hard link, -f": {
			func(dir string) { plain(dir); _ = os.Link(filepath.Join(dir, "f"), filepath.Join(dir, "h")) },
			[]string{"-f", "f"},
		},
		"the recorded name": {
			func(dir string) { _ = os.WriteFile(filepath.Join(dir, "g.gz"), stream(t, payload, "orig"), 0o640) },
			[]string{"-dN", "g.gz"},
		},
		"the recorded name, not": {
			func(dir string) { _ = os.WriteFile(filepath.Join(dir, "g.gz"), stream(t, payload, "orig"), 0o640) },
			[]string{"-d", "g.gz"},
		},
		"a recorded path": {func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, "g.gz"), stream(t, payload, "../up/orig"), 0o640)
		}, []string{"-dN", "g.gz"}},
		"two files, one missing":  {plain, []string{"nothing", "f"}},
		"two files, one suffixed": {func(dir string) { plain(dir); packed("x.gz")(dir) }, []string{"x.gz", "f"}},
	}

	// Every start of a long option's name, to two letters and to three:
	// which name it is, or that it is several, is the tool's whole table.
	for _, prefix := range []string{
		"a", "as", "b", "be", "bi", "c", "co", "d", "de", "f", "fa", "fo", "h", "he", "k", "ke", "l", "li", "lis",
		"lic", "lz", "n", "na", "no", "q", "qu", "r", "re", "rs", "s", "si", "st", "su", "sy", "syn", "t", "te", "to",
		"u", "un", "v", "ve", "ver", "verb", "vers", "x",
	} {
		scenarios["--"+prefix] = scenario{plain, []string{"--" + prefix, "f"}}
	}

	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			theirs, ours := t.TempDir(), t.TempDir()
			scenario.setup(theirs)
			scenario.setup(ours)

			absolute := func(dir string) []string {
				out := slices.Clone(scenario.args)
				for i, arg := range out {
					if !strings.HasPrefix(arg, "-") {
						out[i] = filepath.Join(dir, arg)
					}
				}

				return out
			}

			cmd := exec.CommandContext(t.Context(), system, absolute(theirs)...)

			cmd.Env = append(os.Environ(), "LC_ALL=C", "GZIP=")

			theirOut, theirDiag := &bytes.Buffer{}, &bytes.Buffer{}
			cmd.Stdout, cmd.Stderr = theirOut, theirDiag

			theirStatus := 0
			if err := cmd.Run(); err != nil {
				theirStatus = cmd.ProcessState.ExitCode()
			}

			ourStatus, ourOut, ourDiag := run(t, "gzip", nil, absolute(ours)...)

			// An option this command does not take is refused under its own
			// name: what is compared is that the prefix names that one
			// option for GNU gzip too, not what GNU gzip then does with it.
			if strings.Contains(ourDiag, "unrecognized option") && strings.HasPrefix(scenario.args[0], "--") &&
				!strings.Contains(ourDiag, "'"+scenario.args[0]+"'") {
				if strings.Contains(theirDiag.String(), "ambiguous") ||
					strings.Contains(theirDiag.String(), "unrecognized") {
					t.Errorf("gzip %s\n ours says %q\n GNU  says %q", scenario.args[0], ourDiag, theirDiag.String())
				}

				return
			}

			if ourStatus != theirStatus || !slices.Equal(holding(t, ours), holding(t, theirs)) ||
				(ourOut == "") != (theirOut.Len() == 0) {
				t.Errorf("gzip %s\n ours exits %d, leaves %q, says %q\n GNU  exits %d, leaves %q, says %q",
					strings.Join(scenario.args, " "), ourStatus, holding(t, ours), ourDiag,
					theirStatus, holding(t, theirs), theirDiag.String())
			}
		})
	}
}

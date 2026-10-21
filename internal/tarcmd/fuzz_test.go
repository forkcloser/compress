package tarcmd_test

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/tarcmd"
)

// FuzzExtract: whatever the archive bytes, `tar -xf -` never panics,
// never writes outside the destination directory (a sibling marker
// directory and the parent stay untouched), and exits 0 or 2. Run it with
// -fuzzminimizetime=0: the fuzzing engine's minimizer stalls on a target
// that makes directories, and the run then reports no executions at all.
func FuzzExtract(f *testing.F) {
	for _, build := range []func(w *tar.Writer){
		func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "a.txt", Mode: 0o644, Size: 5, Typeflag: tar.TypeReg})
			_, _ = w.Write([]byte("hello"))
		},
		func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "d/", Mode: 0o755, Typeflag: tar.TypeDir})
			_ = w.WriteHeader(&tar.Header{Name: "d/l", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "../../../etc/passwd"})
			_ = w.WriteHeader(&tar.Header{Name: "d/l/x", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
			_, _ = w.Write([]byte("x"))
		},
		func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
			_, _ = w.Write([]byte("x"))
		},
		func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "/abs", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
			_, _ = w.Write([]byte("x"))
		},
		func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: "h", Mode: 0o644, Typeflag: tar.TypeLink, Linkname: "../../outside"})
		},
		func(w *tar.Writer) {
			_ = w.WriteHeader(&tar.Header{Name: strings.Repeat("n", 150) + "/" + strings.Repeat("m", 120), Mode: 0o644, Size: 0, Typeflag: tar.TypeReg, Format: tar.FormatPAX})
		},
	} {
		var buf bytes.Buffer

		w := tar.NewWriter(&buf)
		build(w)
		_ = w.Close()

		f.Add(buf.Bytes())
	}

	f.Add([]byte{})

	f.Add(make([]byte, 1024))

	root, err := os.MkdirTemp("", "fuzz-extract")
	if err != nil {
		f.Fatal(err)
	}

	f.Cleanup(func() { _ = os.RemoveAll(root) })

	f.Fuzz(func(t *testing.T, archive []byte) {
		// One directory per input under a root made once: a t.TempDir per
		// input is what makes the fuzzer crawl.
		base, err := os.MkdirTemp(root, "in") //nolint:usetesting // a t.TempDir per input makes the fuzzer crawl
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = os.RemoveAll(base) }()

		dest := filepath.Join(base, "dest")
		marker := filepath.Join(base, "marker")

		for _, d := range []string{dest, marker} {
			if mkErr := os.Mkdir(d, 0o755); mkErr != nil {
				t.Fatal(mkErr)
			}
		}

		var stdout, stderr bytes.Buffer

		status := tarcmd.Run([]string{"-xf", "-", "-C", dest}, bytes.NewReader(archive), &stdout, &stderr, "fuzz")
		if status != 0 && status != 2 {
			t.Fatalf("exit %d: %s", status, stderr.String())
		}

		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) != 2 {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}

			t.Fatalf("the parent holds %v, want dest and marker alone", names)
		}

		if inMarker, mErr := os.ReadDir(marker); mErr != nil || len(inMarker) != 0 {
			t.Fatalf("the sibling directory was written: %v %v", inMarker, mErr)
		}
	})
}

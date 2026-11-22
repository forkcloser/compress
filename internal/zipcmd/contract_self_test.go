package zipcmd_test

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/compress/internal/zipcmd"
)

// TestContractArchiveNotInItself: the archive being updated is never one of
// its own inputs, whether -r meets it in the directory it walks, or it is
// named, or reached through a link; named alone, there is nothing to do.
func TestContractArchiveNotInItself(t *testing.T) {
	tree(t)

	const walked = "d/ d/a.txt d/sub/ d/sub/b.txt"

	mustRun(t, "", "-qr", "self.zip", ".")
	mustRun(t, "", "-qr", "self.zip", ".")

	if got := strings.Join(names(readArchiveFile(t, "self.zip")), " "); got != walked {
		t.Errorf("-r . twice: the archive holds %q, want %q", got, walked)
	}

	writeFile(t, "new.txt", "new\n")
	mustRun(t, "", "-q", "self.zip", "self.zip", "new.txt")

	if got := strings.Join(names(readArchiveFile(t, "self.zip")), " "); got != walked+" new.txt" {
		t.Errorf("named beside another input: the archive holds %q, want %q", got, walked+" new.txt")
	}

	inputs := []string{"self.zip"}

	if err := os.Symlink("self.zip", "link"); err == nil {
		inputs = append(inputs, "link")
	} else if runtime.GOOS != "windows" {
		t.Fatal(err) // on Windows a symbolic link needs a privilege the account may lack
	}

	for _, input := range inputs {
		status, _, stderr := run(t, "", "self.zip", input)
		if status != zipcmd.ExitNothing || !strings.Contains(stderr, "zip error: Nothing to do! (self.zip)") {
			t.Errorf("zip self.zip %s: exit %d, stderr %q, want %d and nothing to do",
				input, status, stderr, zipcmd.ExitNothing)
		}
	}

	if got := strings.Join(names(readArchiveFile(t, "self.zip")), " "); got != walked+" new.txt" {
		t.Errorf("named alone: the archive holds %q, want %q", got, walked+" new.txt")
	}
}

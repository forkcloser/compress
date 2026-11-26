package zipcmd_test

// What -r does with a symbolic link to a directory.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/forkcloser/go-compress/internal/reference"
	"github.com/forkcloser/go-compress/internal/zipcmd"
)

// linkOrSkip makes a symbolic link, or skips the test where the account may
// not.
func linkOrSkip(t *testing.T, target, link string) {
	t.Helper()

	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symbolic links need a privilege this account lacks")
		}

		t.Fatal(err)
	}
}

// TestContractLinkToDirectoryIsWalked: -r walks into a symbolic link to a
// directory it meets, as Info-ZIP does without -y: what is under the link
// is stored under the link's name, and nothing is said. The link was stored
// as an empty directory, its files were left out, and the status was 0.
func TestContractLinkToDirectoryIsWalked(t *testing.T) {
	tree(t)
	writeFile(t, filepath.Join("e", "inner.txt"), "inner\n")
	writeFile(t, filepath.Join("e", "deep", "leaf.txt"), "leaf\n")
	linkOrSkip(t, filepath.Join("..", "e"), filepath.Join("d", "elink"))

	status, _, stderr := run(t, "", "-qr", "met.zip", "d")
	if status != zipcmd.ExitOK || stderr != "" {
		t.Errorf("exit %d, stderr %q, want 0 and nothing said", status, stderr)
	}

	entries := readArchiveFile(t, "met.zip")

	want := "d/ d/a.txt d/elink/ d/elink/deep/ d/elink/deep/leaf.txt d/elink/inner.txt d/sub/ d/sub/b.txt"
	if got := strings.Join(names(entries), " "); got != want {
		t.Fatalf("the archive holds %q, want %q", got, want)
	}

	if inner := entries[5]; inner.data != "inner\n" {
		t.Errorf("d/elink/inner.txt holds %q, want the file's content", inner.data)
	}

	// Under -D the directories have no entry, and the files are all there.
	mustRun(t, "", "-qrD", "nodirs.zip", "d")

	want = "d/a.txt d/elink/deep/leaf.txt d/elink/inner.txt d/sub/b.txt"
	if got := strings.Join(names(readArchiveFile(t, "nodirs.zip")), " "); got != want {
		t.Errorf("-D: the archive holds %q, want %q", got, want)
	}
}

// TestContractLinkBackIsNotFollowed: a link that leads back to a directory
// it is under is stored as that directory's entry, reported and not
// entered, so the walk ends whatever the links: one link back, two in one
// directory, a link to the directory above, two directories that link to
// each other. Info-ZIP enters such a link for as deep as the system
// resolves the path, 66 names for the first of these, and with two in one
// directory does not end.
func TestContractLinkBackIsNotFollowed(t *testing.T) {
	tree(t)
	writeFile(t, filepath.Join("e", "inner.txt"), "inner\n")
	linkOrSkip(t, ".", filepath.Join("d", "sub", "back"))
	linkOrSkip(t, ".", filepath.Join("d", "sub", "again"))
	linkOrSkip(t, "..", filepath.Join("d", "sub", "up"))
	linkOrSkip(t, filepath.Join("..", "e"), filepath.Join("d", "toe"))
	linkOrSkip(t, filepath.Join("..", "d"), filepath.Join("e", "tod"))

	status, _, stderr := run(t, "", "-qr", "met.zip", "d")
	if status != zipcmd.ExitOK {
		t.Fatalf("exit %d, want 0\n%s", status, stderr)
	}

	want := []string{
		"d/", "d/a.txt",
		"d/sub/", "d/sub/again/", "d/sub/b.txt", "d/sub/back/", "d/sub/up/",
		"d/toe/", "d/toe/inner.txt", "d/toe/tod/",
	}
	if got := names(readArchiveFile(t, "met.zip")); !slices.Equal(got, want) {
		t.Errorf("the archive holds %q, want %q", got, want)
	}

	for _, link := range []string{
		filepath.Join("d", "sub", "again"), filepath.Join("d", "sub", "back"), filepath.Join("d", "sub", "up"),
		filepath.Join("d", "toe", "tod"),
	} {
		line := "\tzip warning: link back to a directory it is under, not followed: " + link + "\n"
		if strings.Count(stderr, line) != 1 {
			t.Errorf("%q is reported %d times, want once:\n%s", link, strings.Count(stderr, line), stderr)
		}
	}
}

// TestContractInteropLinksToDirectories: the Info-ZIP zip on PATH and this
// one store the same names from a tree with links that lead nowhere back: to
// a directory in the tree, to one outside it, to a file, to nothing.
func TestContractInteropLinksToDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the zip on a Windows runner's PATH is not one this comparison was measured against")
	}

	tree(t)
	writeFile(t, filepath.Join("e", "inner.txt"), "inner\n")
	linkOrSkip(t, filepath.Join("..", "e"), filepath.Join("d", "out"))
	linkOrSkip(t, "sub", filepath.Join("d", "within"))
	linkOrSkip(t, "a.txt", filepath.Join("d", "file"))
	linkOrSkip(t, "nothing", filepath.Join("d", "dangling"))

	theirZip := reference.Find(t, "zip", "-v")

	if out, err := exec.CommandContext(t.Context(), theirZip, "-qr", "theirs.zip", "d").CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", theirZip, err, out)
	}

	if status, _, stderr := run(t, "", "-qr", "ours.zip", "d"); status != zipcmd.ExitOK {
		t.Fatalf("exit %d, want 0\n%s", status, stderr)
	}

	theirs := names(readArchiveFile(t, "theirs.zip"))
	ours := names(readArchiveFile(t, "ours.zip"))

	slices.Sort(theirs)
	slices.Sort(ours)

	if !slices.Contains(theirs, "d/out/inner.txt") || !slices.Contains(theirs, "d/within/b.txt") {
		t.Fatalf("%s did not follow the links: %q", theirZip, theirs)
	}

	if !slices.Equal(ours, theirs) {
		t.Errorf("%d names stored, %s stores %d\nours:   %q\ntheirs: %q",
			len(ours), theirZip, len(theirs), ours, theirs)
	}
}

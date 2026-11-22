package unzipcmd_test

import (
	"archive/zip"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The DOS attributes an entry written without unix modes carries.
const (
	dosReadOnly  = 0x01
	dosDirectory = 0x10
)

// dos is an entry as archive/zip's Create, Java and Windows tools write
// it: the creator is FAT and the external attributes are DOS bits, with no
// unix mode anywhere.
func dos(name, content string, attrs uint32) raw {
	header := zip.FileHeader{Name: name, Method: zip.Store, UncompressedSize64: uint64(len(content))}
	header.CRC32 = crc32.ChecksumIEEE([]byte(content))
	header.Modified = stamp()
	header.ExternalAttrs = attrs

	return raw{header, content}
}

// unixHalf is a regular file's unix mode where a unix creator stores it.
func unixHalf(perm uint32) uint32 {
	return (0o100000 | perm) << 16
}

// created is the entry as another creator's.
func created(creator uint16, entry raw) raw {
	entry.header.CreatorVersion = creator << 8

	return entry
}

// modeless writes an archive without unix modes: a directory entry that
// has only its trailing slash, one with the DOS directory bit, a read-only
// one, and files in them, one of them read-only. Three more files carry a
// unix mode that is not theirs to carry: the creator is not unix (19, NTFS
// 11), or it is FAT and the mode contradicts the DOS bits. The last is a
// unix tool's that signs as FAT, the mode agreeing with the DOS bits.
func modeless(t *testing.T, dir string) string {
	t.Helper()

	return buildRaw(t, filepath.Join(dir, "dos.zip"),
		dos("pkg/", "", 0),
		dos("pkg/tool.txt", "tool", 0),
		dos("bits/", "", dosDirectory),
		dos("locked/", "", dosDirectory|dosReadOnly),
		dos("locked/in.txt", "in", 0),
		dos("top.txt", "top", 0),
		dos("fixed.txt", "fixed", dosReadOnly),
		created(19, dos("darwin.txt", "darwin", unixHalf(0o600))),
		created(11, dos("ntfs.txt", "ntfs", unixHalf(0o600))),
		dos("contradicted.txt", "contradicted", unixHalf(0o400)),
		dos("pkzip.txt", "pkzip", unixHalf(0o600)),
	)
}

// perms is the permission bits of everything under root, by slash path.
func perms(t *testing.T, root string) map[string]fs.FileMode {
	t.Helper()

	// A directory without its write bits is still walked; one made without
	// its search bit is not, and the walk says which.
	found := map[string]fs.FileMode{}

	err := filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if rel, _ := filepath.Rel(root, path); rel != "." {
			found[filepath.ToSlash(rel)] = info.Mode().Perm()
		}

		return nil
	})
	if err != nil {
		t.Error(err)
	}

	return found
}

// unlock gives the owner every directory under root back, so that the
// test's directory can be removed.
func unlock(t *testing.T, root string) {
	t.Helper()

	t.Cleanup(func() {
		_ = filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
			if err == nil && info.IsDir() {
				_ = os.Chmod(path, 0o755)
			}

			return nil
		})
	})
}

// TestContractNoUnixModes: an entry written where there are no unix modes
// gets everything the umask lets through, as a file or a directory made
// here does, less the write bits when it is read-only in the archive; it
// is not given 0666 past the umask, and a directory stays one its owner
// can enter. A unix mode counts from a unix creator, and from a FAT one
// when its owner bits say what the DOS bits say; then it is set exactly.
func TestContractNoUnixModes(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("no permission bits")
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	archive := modeless(t, dir)

	unlock(t, out)
	mustRun(t, "-q", archive, "-d", out)

	// What the umask leaves of a new file and a new directory.
	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o666); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(dir, "directory"), 0o777); err != nil {
		t.Fatal(err)
	}

	made := perms(t, dir)
	file, directory := made["file"], made["directory"]

	want := map[string]fs.FileMode{
		"pkg":           directory,
		"pkg/tool.txt":  file,
		"bits":          directory,
		"locked":        directory &^ 0o222,
		"locked/in.txt": file,
		"top.txt":       file,
		"fixed.txt":     file &^ 0o222,

		"darwin.txt":       file,
		"ntfs.txt":         file,
		"contradicted.txt": file,
		"pkzip.txt":        0o600,
	}

	got := perms(t, out)
	for name, mode := range want {
		if got[name] != mode {
			t.Errorf("%s is %v, want %v", name, got[name], mode)
		}
	}

	if len(got) != len(want) {
		t.Errorf("extracted %v", got)
	}
}

// TestContractNoUnixModesAsInfoZIP: the modes of what such an archive
// extracts to are the ones the Info-ZIP unzip gives them, under the same
// umask.
func TestContractNoUnixModesAsInfoZIP(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("no permission bits")
	}

	dir := t.TempDir()
	ours, reference := filepath.Join(dir, "ours"), filepath.Join(dir, "theirs")
	archive := modeless(t, dir)

	unlock(t, dir)

	if status, _, stderr := theirs(t, "-q", archive, "-d", reference); status != 0 {
		t.Fatalf("Info-ZIP: exit %d\n%s", status, stderr)
	}

	mustRun(t, "-q", archive, "-d", ours)

	want, got := perms(t, reference), perms(t, ours)
	for name, mode := range want {
		if got[name] != mode {
			t.Errorf("%s is %v, Info-ZIP's is %v", name, got[name], mode)
		}
	}

	if len(got) != len(want) {
		t.Errorf("extracted %v, Info-ZIP %v", got, want)
	}
}

package sparse

import (
	"os"
	"syscall"
)

// fsctlSetSparse is FSCTL_SET_SPARSE.
const fsctlSetSparse = 0x000900c4

// markSparse tells the filesystem that file has holes: without it, NTFS
// writes the zeros of every stretch a write steps over.
func markSparse(file *os.File) {
	var returned uint32

	_ = syscall.DeviceIoControl(syscall.Handle(file.Fd()), fsctlSetSparse, nil, 0, nil, 0, &returned, nil)
}

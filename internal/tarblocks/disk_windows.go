package tarblocks

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	// invalidSize is INVALID_FILE_SIZE, which GetCompressedFileSizeW
	// returns for a failure and for a size that ends in those 32 bits.
	invalidSize = 0xffffffff
	wordBits    = 32
)

//nolint:gochecknoglobals // the one call the standard library does not wrap
var compressedSize = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCompressedFileSizeW")

// OnDisk is what the file at path takes on disk: less than its length when
// it has holes.
func OnDisk(path string) (int64, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("name %s: %w", path, err)
	}

	var high uint32

	// #nosec G103 -- GetCompressedFileSizeW takes a name and where to put the high half
	low, _, callErr := compressedSize.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&high)))
	if low == invalidSize && !errors.Is(callErr, syscall.Errno(0)) {
		return 0, fmt.Errorf("size of %s: %w", path, callErr)
	}

	// #nosec G115 -- the two halves of a file's size
	return int64(high)<<wordBits | int64(low), nil
}

//go:build unix

package tarblocks

import (
	"fmt"
	"syscall"
)

// statBlock is the unit of a stat's block count, whatever the filesystem's.
const statBlock = 512

// OnDisk is what the file at path takes on disk: less than its length when
// it has holes.
func OnDisk(path string) (int64, error) {
	var stat syscall.Stat_t
	if err := syscall.Lstat(path, &stat); err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}

	return stat.Blocks * statBlock, nil
}

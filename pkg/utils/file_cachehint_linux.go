//go:build linux

package utils

import "golang.org/x/sys/unix"

// DropFileCacheByFD asks kernel to drop cached pages of a file range.
// length <= 0 means from offset to EOF.
func DropFileCacheByFD(fd uintptr, offset, length int64) error {
	return unix.Fadvise(int(fd), offset, length, unix.FADV_DONTNEED)
}

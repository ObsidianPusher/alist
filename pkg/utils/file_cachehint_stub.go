//go:build !linux

package utils

// DropFileCacheByFD is best-effort on Linux and a no-op on other OSes.
func DropFileCacheByFD(fd uintptr, offset, length int64) error {
	return nil
}

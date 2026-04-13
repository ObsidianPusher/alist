//go:build cgo && fuse
// +build cgo,fuse

package fuse

import "github.com/winfsp/cgofuse/fuse"

type Fs struct {
	RootFolder string
	fuse.FileSystemBase
}

func unsupported() int {
	return -fuse.ENOSYS
}

func unsupportedHandle() (int, uint64) {
	return -fuse.ENOSYS, ^uint64(0)
}

func (fs *Fs) Init() {
	// keep no-op until FUSE filesystem is implemented.
}

func (fs *Fs) Destroy() {
	// keep no-op until FUSE filesystem is implemented.
}

func (fs *Fs) Statfs(path string, stat *fuse.Statfs_t) int {
	return unsupported()
}

func (fs *Fs) Mknod(path string, mode uint32, dev uint64) int {
	return unsupported()
}

func (fs *Fs) Mkdir(path string, mode uint32) int {
	return unsupported()
}

func (fs *Fs) Unlink(path string) int {
	return unsupported()
}

func (fs *Fs) Rmdir(path string) int {
	return unsupported()
}

func (fs *Fs) Link(oldpath string, newpath string) int {
	return unsupported()
}

func (fs *Fs) Symlink(target string, newpath string) int {
	return unsupported()
}

func (fs *Fs) Readlink(path string) (int, string) {
	return unsupported(), ""
}

func (fs *Fs) Rename(oldpath string, newpath string) int {
	return unsupported()
}

func (fs *Fs) Chmod(path string, mode uint32) int {
	return unsupported()
}

func (fs *Fs) Chown(path string, uid uint32, gid uint32) int {
	return unsupported()
}

func (fs *Fs) Utimens(path string, tmsp []fuse.Timespec) int {
	return unsupported()
}

func (fs *Fs) Access(path string, mask uint32) int {
	return unsupported()
}

func (fs *Fs) Create(path string, flags int, mode uint32) (int, uint64) {
	return unsupportedHandle()
}

func (fs *Fs) Open(path string, flags int) (int, uint64) {
	return unsupportedHandle()
}

func (fs *Fs) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Truncate(path string, size int64, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Read(path string, buff []byte, ofst int64, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Write(path string, buff []byte, ofst int64, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Flush(path string, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Release(path string, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Fsync(path string, datasync bool, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Opendir(path string) (int, uint64) {
	return unsupportedHandle()
}

func (fs *Fs) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Releasedir(path string, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Fsyncdir(path string, datasync bool, fh uint64) int {
	return unsupported()
}

func (fs *Fs) Setxattr(path string, name string, value []byte, flags int) int {
	return unsupported()
}

func (fs *Fs) Getxattr(path string, name string) (int, []byte) {
	return unsupported(), nil
}

func (fs *Fs) Removexattr(path string, name string) int {
	return unsupported()
}

func (fs *Fs) Listxattr(path string, fill func(name string) bool) int {
	return unsupported()
}

var _ fuse.FileSystemInterface = (*Fs)(nil)

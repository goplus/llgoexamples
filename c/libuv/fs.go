package libuv

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	_ "github.com/goplus/llgo/c/libuv"
)

type FsType = libuv.FsType
type DirentType = libuv.DirentType
type UvFile = libuv.UvFile

type Fs struct {
	*libuv.Fs
}

type FsEvent struct {
	*libuv.FsEvent
}

type FsPoll struct {
	*libuv.FsPoll
}

type Dirent struct {
	*libuv.Dirent
}

type Stat struct {
	*libuv.Stat
}

type FsCb func(req *Fs)
type FsEventCb func(handle *FsEvent, filename *c.Char, events c.Int, status c.Int)
type FsPollCb func(handle *FsPoll, status c.Int, events c.Int)

func convertFsCb(callback FsCb) func(*libuv.Fs) {
	return func(libuvFs *libuv.Fs) {
		fs := &Fs{Fs: libuvFs}
		callback(fs)
	}
}

func convertFsEventCb(callback FsEventCb) func(*libuv.FsEvent, *c.Char, c.Int, c.Int) {
	return func(handle *libuv.FsEvent, filename *c.Char, events c.Int, status c.Int) {
		fsEvent := &FsEvent{FsEvent: handle}
		callback(fsEvent, filename, events, status)
	}
}

func convertFsPollCb(callback FsPollCb) func(*libuv.FsPoll, c.Int, c.Int) {
	return func(handle *libuv.FsPoll, status c.Int, events c.Int) {
		fsPoll := &FsPoll{FsPoll: handle}
		callback(fsPoll, status, events)
	}

}

// GetType Get the type of the file system request.
func (f *Fs) GetType() *FsType {
	return libuv.FsGetType(f.Fs)
}

// GetPath Get the path of the file system request.
func (f *Fs) GetPath() string {
	return c.GoString(libuv.FsGetPath(f.Fs))
}

// GetResult Get the result of the file system request.
func (f *Fs) GetResult() int {
	return int(libuv.FsGetResult(f.Fs))
}

// GetPtr Get the pointer of the file system request.
func (f *Fs) GetPtr() c.Pointer {
	return libuv.FsGetPtr(f.Fs)
}

// GetSystemError Get the system error of the file system request.
func (f *Fs) GetSystemError() int {
	return int(libuv.FsGetSystemError(f.Fs))
}

// GetStatBuf Get the stat buffer of the file system request.
func (f *Fs) GetStatBuf() *libuv.Stat {
	return libuv.FsGetStatBuf(f.Fs)
}

// Cleanup cleans up the file system request.
func (f *Fs) Cleanup() {
	libuv.FsReqCleanup(f.Fs)
}

// Open opens a file specified by the path with given flags and mode, and returns a file descriptor.
func (f *Fs) Open(loop *Loop, path string, flags int, mode int, cb FsCb) int {
	return int(libuv.FsOpen(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(flags), c.Int(mode), convertFsCb(cb)))
}

// Close closes a file descriptor.
func (f *Fs) Close(loop *Loop, file int, cb FsCb) int {
	return int(libuv.FsClose(loop.Loop, f.Fs, UvFile(file), convertFsCb(cb)))
}

// Read reads data from a file descriptor into a buffer at a specified offset.
func (f *Fs) Read(loop *Loop, file int, bufs Buf, nbufs c.Uint, offset int64, cb FsCb) int {
	return int(libuv.FsRead(loop.Loop, f.Fs, UvFile(file), bufs.Buf, nbufs, c.LongLong(offset), convertFsCb(cb)))
}

// Write writes data to a file descriptor from a buffer at a specified offset.
func (f *Fs) Write(loop *Loop, file int, bufs Buf, nbufs c.Uint, offset int64, cb FsCb) int {
	return int(libuv.FsWrite(loop.Loop, f.Fs, UvFile(file), bufs.Buf, nbufs, c.LongLong(offset), convertFsCb(cb)))
}

// Unlink deletes a file specified by the path.
func (f *Fs) Unlink(loop *Loop, path string, cb FsCb) int {
	return int(libuv.FsUnlink(loop.Loop, f.Fs, c.AllocaCStr(path), convertFsCb(cb)))
}

// Mkdir creates a new directory at the specified path with a specified mode.
func (f *Fs) Mkdir(loop *Loop, path string, mode int, cb FsCb) int {
	return int(libuv.FsMkdir(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(mode), convertFsCb(cb)))
}

// Mkdtemp creates a temporary directory with a template path.
func (f *Fs) Mkdtemp(loop *Loop, tpl string, cb FsCb) int {
	return int(libuv.FsMkdtemp(loop.Loop, f.Fs, c.AllocaCStr(tpl), convertFsCb(cb)))
}

// MkStemp creates a temporary file from a template path.
func (f *Fs) MkStemp(loop *Loop, tpl string, cb FsCb) int {
	return int(libuv.FsMkStemp(loop.Loop, f.Fs, c.AllocaCStr(tpl), convertFsCb(cb)))
}

// Rmdir removes a directory specified by the path.
func (f *Fs) Rmdir(loop *Loop, path string, cb FsCb) int {
	return int(libuv.FsRmdir(loop.Loop, f.Fs, c.AllocaCStr(path), convertFsCb(cb)))
}

// Stat retrieves status information about the file specified by the path.
func (f *Fs) Stat(loop *Loop, path string, cb FsCb) int {
	return int(libuv.FsStat(loop.Loop, f.Fs, c.AllocaCStr(path), convertFsCb(cb)))
}

// Fstat retrieves status information about a file descriptor.
func (f *Fs) Fstat(loop *Loop, file int, cb FsCb) int {
	return int(libuv.FsFstat(loop.Loop, f.Fs, UvFile(file), convertFsCb(cb)))
}

// Rename renames a file from the old path to the new path.
func (f *Fs) Rename(loop *Loop, path string, newPath string, cb FsCb) int {
	return int(libuv.FsRename(loop.Loop, f.Fs, c.AllocaCStr(path), c.AllocaCStr(newPath), convertFsCb(cb)))
}

// Fsync synchronizes a file descriptor's state with storage device.
func (f *Fs) Fsync(loop *Loop, file int, cb FsCb) int {
	return int(libuv.FsFsync(loop.Loop, f.Fs, UvFile(file), convertFsCb(cb)))
}

// Fdatasync synchronizes a file descriptor's data with storage device.
func (f *Fs) Fdatasync(loop *Loop, file int, cb FsCb) int {
	return int(libuv.FsFdatasync(loop.Loop, f.Fs, UvFile(file), convertFsCb(cb)))
}

// Ftruncate truncates a file to a specified length.
func (f *Fs) Ftruncate(loop *Loop, file int, offset int64, cb FsCb) int {
	return int(libuv.FsFtruncate(loop.Loop, f.Fs, UvFile(file), c.LongLong(offset), convertFsCb(cb)))
}

// Sendfile sends data from one file descriptor to another.
func (f *Fs) Sendfile(loop *Loop, outFd int, inFd int, inOffset int64, length int, cb FsCb) int {
	return int(libuv.FsSendfile(loop.Loop, f.Fs, c.Int(outFd), c.Int(inFd), c.LongLong(inOffset), c.Int(length), convertFsCb(cb)))
}

// Access checks the access permissions of a file specified by the path.
func (f *Fs) Access(loop *Loop, path string, flags int, cb FsCb) int {
	return int(libuv.FsAccess(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(flags), convertFsCb(cb)))
}

// Chmod changes the permissions of a file specified by the path.
func (f *Fs) Chmod(loop *Loop, path string, mode int, cb FsCb) int {
	return int(libuv.FsChmod(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(mode), convertFsCb(cb)))
}

// Fchmod changes the permissions of a file descriptor.
func (f *Fs) Fchmod(loop *Loop, file int, mode int, cb FsCb) int {
	return int(libuv.FsFchmod(loop.Loop, f.Fs, UvFile(file), c.Int(mode), convertFsCb(cb)))
}

// Utime updates the access and modification times of a file specified by the path.
func (f *Fs) Utime(loop *Loop, path string, atime int, mtime int, cb FsCb) int {
	return int(libuv.FsUtime(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(atime), c.Int(mtime), convertFsCb(cb)))
}

// Futime updates the access and modification times of a file descriptor.
func (f *Fs) Futime(loop *Loop, file int, atime int, mtime int, cb FsCb) int {
	return int(libuv.FsFutime(loop.Loop, f.Fs, UvFile(file), c.Int(atime), c.Int(mtime), convertFsCb(cb)))
}

// Lutime updates the access and modification times of a file specified by the path, even if the path is a symbolic link.
func (f *Fs) Lutime(loop *Loop, path string, atime int, mtime int, cb FsCb) int {
	return int(libuv.FsLutime(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(atime), c.Int(mtime), convertFsCb(cb)))
}

// Link creates a new link to an existing file.
func (f *Fs) Link(loop *Loop, path string, newPath string, cb FsCb) int {
	return int(libuv.FsLink(loop.Loop, f.Fs, c.AllocaCStr(path), c.AllocaCStr(newPath), convertFsCb(cb)))
}

// Symlink creates a symbolic link from the path to the new path.
func (f *Fs) Symlink(loop *Loop, path string, newPath string, flags int, cb FsCb) int {
	return int(libuv.FsSymlink(loop.Loop, f.Fs, c.AllocaCStr(path), c.AllocaCStr(newPath), c.Int(flags), convertFsCb(cb)))
}

// Readlink reads the target of a symbolic link.
func (f *Fs) Readlink(loop *Loop, path string, cb FsCb) int {
	return int(libuv.FsReadlink(loop.Loop, f.Fs, c.AllocaCStr(path), convertFsCb(cb)))
}

// Realpath resolves the absolute path of a file.
func (f *Fs) Realpath(loop *Loop, path string, cb FsCb) int {
	return int(libuv.FsRealpath(loop.Loop, f.Fs, c.AllocaCStr(path), convertFsCb(cb)))
}

// Copyfile copies a file from the source path to the destination path.
func (f *Fs) Copyfile(loop *Loop, path string, newPath string, flags int, cb FsCb) int {
	return int(libuv.FsCopyfile(loop.Loop, f.Fs, c.AllocaCStr(path), c.AllocaCStr(newPath), c.Int(flags), convertFsCb(cb)))
}

// Scandir scans a directory for entries.
func (f *Fs) Scandir(loop *Loop, path string, flags int, cb FsCb) int {
	return int(libuv.FsScandir(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(flags), convertFsCb(cb)))
}

// OpenDir opens a directory specified by the path.
func (f *Fs) OpenDir(loop *Loop, path string, cb FsCb) int {
	return int(libuv.FsOpenDir(loop.Loop, f.Fs, c.AllocaCStr(path), convertFsCb(cb)))
}

// Readdir reads entries from an open directory.
func (f *Fs) Readdir(loop *Loop, dir int, cb FsCb) int {
	return int(libuv.FsReaddir(loop.Loop, f.Fs, c.Int(dir), convertFsCb(cb)))
}

// CloseDir closes an open directory.
func (f *Fs) CloseDir(loop *Loop) int {
	return int(libuv.FsCloseDir(loop.Loop, f.Fs))
}

// Statfs retrieves file system status information.
func (f *Fs) Statfs(loop *Loop, path string, cb FsCb) int {
	return int(libuv.FsStatfs(loop.Loop, f.Fs, c.AllocaCStr(path), convertFsCb(cb)))
}

// Chown Change file ownership
func (f *Fs) Chown(loop *Loop, path string, uid int, gid int, cb FsCb) int {
	return int(libuv.FsChown(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(uid), c.Int(gid), convertFsCb(cb)))
}

// Fchown Change file ownership by file descriptor
func (f *Fs) Fchown(loop *Loop, file int, uid int, gid int, cb FsCb) int {
	return int(libuv.FsFchown(loop.Loop, f.Fs, UvFile(file), c.Int(uid), c.Int(gid), convertFsCb(cb)))
}

// Lchown Change file ownership (symlink)
func (f *Fs) Lchown(loop *Loop, path string, uid int, gid int, cb FsCb) int {
	return int(libuv.FsLchown(loop.Loop, f.Fs, c.AllocaCStr(path), c.Int(uid), c.Int(gid), convertFsCb(cb)))
}

// Lstat Get file status (symlink)
func (f *Fs) Lstat(loop *Loop, path string, cb FsCb) int {
	return int(libuv.FsLstat(loop.Loop, f.Fs, c.AllocaCStr(path), convertFsCb(cb)))
}

// Init Initialize a file event handle
func (e *FsEvent) Init(loop *Loop) int {
	return int(libuv.FsEventInit(loop.Loop, e.FsEvent))
}

// Start listening for file events
func (e *FsEvent) Start(cb FsEventCb, path string, flags int) int {
	return int(libuv.FsEventStart(e.FsEvent, convertFsEventCb(cb), c.AllocaCStr(path), c.Int(flags)))
}

// Stop listening for file events
func (e *FsEvent) Stop() int {
	return int(libuv.FsEventStop(e.FsEvent))
}

// Close the file event handle
func (e *FsEvent) Close() int {
	return int(libuv.FsEventClose(e.FsEvent))
}

// GetPath Get the path of the file event
func (e *FsEvent) GetPath() *c.Char {
	return libuv.FsEventGetpath(e.FsEvent)
}

// Init Initialize a file poll handle
func (p *FsPoll) Init(loop *Loop) int {
	return int(libuv.FsPollInit(loop.Loop, p.FsPoll))
}

// Start polling for file changes
func (p *FsPoll) Start(cb FsPollCb, path string, interval uint) int {
	return int(libuv.FsPollStart(p.FsPoll, convertFsPollCb(cb), c.AllocaCStr(path), interval))
}

// Stop polling for file changes
func (p *FsPoll) Stop() int {
	return int(libuv.FsPollStop(p.FsPoll))
}

// Close the file poll handle
func (p *FsPoll) Close() int {
	return int(libuv.FsPollClose(p.FsPoll))
}

// GetPath Get the path of the file poll
func (p *FsPoll) GetPath() string {
	return c.GoString(libuv.FsPollGetPath(p.FsPoll))
}

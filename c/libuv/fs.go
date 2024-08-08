package libuv

import (
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
)

const (
	FS_UNKNOWN   = libuv.FS_UNKNOWN
	FS_CUSTOM    = libuv.FS_CUSTOM
	FS_OPEN      = libuv.FS_OPEN
	FS_CLOSE     = libuv.FS_CLOSE
	FS_READ      = libuv.FS_READ
	FS_WRITE     = libuv.FS_WRITE
	FS_SENDFILE  = libuv.FS_SENDFILE
	FS_STAT      = libuv.FS_STAT
	FS_LSTAT     = libuv.FS_LSTAT
	FS_FSTAT     = libuv.FS_FSTAT
	FS_FTRUNCATE = libuv.FS_FTRUNCATE
	FS_UTIME     = libuv.FS_UTIME
	FS_FUTIME    = libuv.FS_FUTIME
	FS_ACCESS    = libuv.FS_ACCESS
	FS_CHMOD     = libuv.FS_CHMOD
	FS_FCHMOD    = libuv.FS_FCHMOD
	FS_FSYNC     = libuv.FS_FSYNC
	FS_FDATASYNC = libuv.FS_FDATASYNC
	FS_UNLINK    = libuv.FS_UNLINK
	FS_RMDIR     = libuv.FS_RMDIR
	FS_MKDIR     = libuv.FS_MKDIR
	FS_MKDTEMP   = libuv.FS_MKDTEMP
	FS_RENAME    = libuv.FS_RENAME
	FS_SCANDIR   = libuv.FS_SCANDIR
	FS_LINK      = libuv.FS_LINK
	FS_SYMLINK   = libuv.FS_SYMLINK
	FS_READLINK  = libuv.FS_READLINK
	FS_CHOWN     = libuv.FS_CHOWN
	FS_FCHOWN    = libuv.FS_FCHOWN
	FS_REALPATH  = libuv.FS_REALPATH
	FS_COPYFILE  = libuv.FS_COPYFILE
	FS_LCHOWN    = libuv.FS_LCHOWN
	FS_OPENDIR   = libuv.FS_OPENDIR
	FS_READDIR   = libuv.FS_READDIR
	FS_CLOSEDIR  = libuv.FS_CLOSEDIR
	FS_STATFS    = libuv.FS_STATFS
	FS_MKSTEMP   = libuv.FS_MKSTEMP
	FS_LUTIME    = libuv.FS_LUTIME
)

const (
	DirentUnknown = libuv.DirentUnknown
	DirentFile    = libuv.DirentFile
	DirentDir     = libuv.DirentDir
	DirentLink    = libuv.DirentLink
	DirentFifo    = libuv.DirentFifo
	DirentSocket  = libuv.DirentSocket
	DirentChar    = libuv.DirentChar
	DirentBlock   = libuv.DirentBlock
)

type Fs struct {
	libuv.Fs
	FsCb FsCb
}

type FsEvent struct {
	*libuv.FsEvent
	FsEventCb FsEventCb
}

type FsPoll struct {
	*libuv.FsPoll
	FsPollCb FsPollCb
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

func NewFs() Fs {
	newFs := libuv.Fs{}
	return Fs{Fs: newFs}
}

// GetType Get the type of the file system request.
func (f *Fs) GetType() libuv.FsType {
	return f.Fs.GetType()
}

// GetPath Get the path of the file system request.
func (f *Fs) GetPath() string {
	return c.GoString(f.Fs.GetPath())
}

// GetResult Get the result of the file system request.
func (f *Fs) GetResult() int {
	return int(f.Fs.GetResult())
}

// GetPtr Get the pointer of the file system request.
func (f *Fs) GetPtr() c.Pointer {
	return f.Fs.GetPtr()
}

// GetSystemError Get the system error of the file system request.
func (f *Fs) GetSystemError() int {
	return int(f.Fs.GetSystemError())
}

// GetStatBuf Get the stat buffer of the file system request.
func (f *Fs) GetStatBuf() *libuv.Stat {
	return f.Fs.GetStatBuf()
}

// Cleanup cleans up the file system request.
func (f *Fs) Cleanup() {
	f.Fs.ReqCleanup()
}

// Open opens a file specified by the path with given flags and mode, and returns a file descriptor.
func (f *Fs) Open(loop *Loop, path string, flags int, mode int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsOpen(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(flags), c.Int(mode), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Close closes a file descriptor.
func (f *Fs) Close(loop *Loop, file int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsClose(loop.Loop, &f.Fs, libuv.File(file), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Read reads data from a file descriptor into a buffer at a specified offset.
func (f *Fs) Read(loop *Loop, file int, bufs Buf, nbufs c.Uint, offset int64, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsRead(loop.Loop, &f.Fs, libuv.File(file), bufs.Buf, nbufs, c.LongLong(offset), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Write writes data to a file descriptor from a buffer at a specified offset.
func (f *Fs) Write(loop *Loop, file int, bufs Buf, nbufs c.Uint, offset int64, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsWrite(loop.Loop, &f.Fs, libuv.File(file), bufs.Buf, nbufs, c.LongLong(offset), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Unlink deletes a file specified by the path.
func (f *Fs) Unlink(loop *Loop, path string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsUnlink(loop.Loop, &f.Fs, c.AllocaCStr(path), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Mkdir creates a new directory at the specified path with a specified mode.
func (f *Fs) Mkdir(loop *Loop, path string, mode int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsMkdir(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(mode), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Mkdtemp creates a temporary directory with a template path.
func (f *Fs) Mkdtemp(loop *Loop, tpl string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsMkdtemp(loop.Loop, &f.Fs, c.AllocaCStr(tpl), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// MkStemp creates a temporary file from a template path.
func (f *Fs) MkStemp(loop *Loop, tpl string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsMkStemp(loop.Loop, &f.Fs, c.AllocaCStr(tpl), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Rmdir removes a directory specified by the path.
func (f *Fs) Rmdir(loop *Loop, path string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsRmdir(loop.Loop, &f.Fs, c.AllocaCStr(path), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Stat retrieves status information about the file specified by the path.
func (f *Fs) Stat(loop *Loop, path string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsStat(loop.Loop, &f.Fs, c.AllocaCStr(path), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Fstat retrieves status information about a file descriptor.
func (f *Fs) Fstat(loop *Loop, file int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsFstat(loop.Loop, &f.Fs, libuv.File(file), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Rename renames a file from the old path to the new path.
func (f *Fs) Rename(loop *Loop, path string, newPath string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsRename(loop.Loop, &f.Fs, c.AllocaCStr(path), c.AllocaCStr(newPath), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Fsync synchronizes a file descriptor's state with storage device.
func (f *Fs) Fsync(loop *Loop, file int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsFsync(loop.Loop, &f.Fs, libuv.File(file), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Fdatasync synchronizes a file descriptor's data with storage device.
func (f *Fs) Fdatasync(loop *Loop, file int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsFdatasync(loop.Loop, &f.Fs, libuv.File(file), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Ftruncate truncates a file to a specified length.
func (f *Fs) Ftruncate(loop *Loop, file int, offset int64, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsFtruncate(loop.Loop, &f.Fs, libuv.File(file), c.LongLong(offset), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Sendfile sends data from one file descriptor to another.
func (f *Fs) Sendfile(loop *Loop, outFd int, inFd int, inOffset int64, length int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsSendfile(loop.Loop, &f.Fs, c.Int(outFd), c.Int(inFd), c.LongLong(inOffset), c.Int(length), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Access checks the access permissions of a file specified by the path.
func (f *Fs) Access(loop *Loop, path string, flags int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsAccess(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(flags), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Chmod changes the permissions of a file specified by the path.
func (f *Fs) Chmod(loop *Loop, path string, mode int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsChmod(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(mode), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Fchmod changes the permissions of a file descriptor.
func (f *Fs) Fchmod(loop *Loop, file int, mode int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsFchmod(loop.Loop, &f.Fs, libuv.File(file), c.Int(mode), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Utime updates the access and modification times of a file specified by the path.
func (f *Fs) Utime(loop *Loop, path string, atime int, mtime int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsUtime(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(atime), c.Int(mtime), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Futime updates the access and modification times of a file descriptor.
func (f *Fs) Futime(loop *Loop, file int, atime int, mtime int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsFutime(loop.Loop, &f.Fs, libuv.File(file), c.Int(atime), c.Int(mtime), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Lutime updates the access and modification times of a file specified by the path, even if the path is a symbolic link.
func (f *Fs) Lutime(loop *Loop, path string, atime int, mtime int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsLutime(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(atime), c.Int(mtime), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Link creates a new link to an existing file.
func (f *Fs) Link(loop *Loop, path string, newPath string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsLink(loop.Loop, &f.Fs, c.AllocaCStr(path), c.AllocaCStr(newPath), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Symlink creates a symbolic link from the path to the new path.
func (f *Fs) Symlink(loop *Loop, path string, newPath string, flags int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsSymlink(loop.Loop, &f.Fs, c.AllocaCStr(path), c.AllocaCStr(newPath), c.Int(flags), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Readlink reads the target of a symbolic link.
func (f *Fs) Readlink(loop *Loop, path string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsReadlink(loop.Loop, &f.Fs, c.AllocaCStr(path), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Realpath resolves the absolute path of a file.
func (f *Fs) Realpath(loop *Loop, path string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsRealpath(loop.Loop, &f.Fs, c.AllocaCStr(path), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Copyfile copies a file from the source path to the destination path.
func (f *Fs) Copyfile(loop *Loop, path string, newPath string, flags int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsCopyfile(loop.Loop, &f.Fs, c.AllocaCStr(path), c.AllocaCStr(newPath), c.Int(flags), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Scandir scans a directory for entries.
func (f *Fs) Scandir(loop *Loop, path string, flags int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsScandir(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(flags), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// OpenDir opens a directory specified by the path.
func (f *Fs) OpenDir(loop *Loop, path string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsOpenDir(loop.Loop, &f.Fs, c.AllocaCStr(path), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Readdir reads entries from an open directory.
func (f *Fs) Readdir(loop *Loop, dir int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsReaddir(loop.Loop, &f.Fs, c.Int(dir), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// CloseDir closes an open directory.
func (f *Fs) CloseDir(loop *Loop) int {
	return int(libuv.FsCloseDir(loop.Loop, &f.Fs))
}

// Statfs retrieves file system status information.
func (f *Fs) Statfs(loop *Loop, path string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsStatfs(loop.Loop, &f.Fs, c.AllocaCStr(path), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Chown Change file ownership
func (f *Fs) Chown(loop *Loop, path string, uid int, gid int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsChown(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(uid), c.Int(gid), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Fchown Change file ownership by file descriptor
func (f *Fs) Fchown(loop *Loop, file int, uid int, gid int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsFchown(loop.Loop, &f.Fs, libuv.File(file), c.Int(uid), c.Int(gid), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Lchown Change file ownership (symlink)
func (f *Fs) Lchown(loop *Loop, path string, uid int, gid int, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsLchown(loop.Loop, &f.Fs, c.AllocaCStr(path), c.Int(uid), c.Int(gid), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Lstat Get file status (symlink)
func (f *Fs) Lstat(loop *Loop, path string, cb FsCb) int {
	f.FsCb = cb
	return int(libuv.FsLstat(loop.Loop, &f.Fs, c.AllocaCStr(path), func(_fs *libuv.Fs) {
		fs := (*Fs)(unsafe.Pointer(_fs))
		fs.FsCb(fs)
	}))
}

// Init Initialize a file event handle
func (e *FsEvent) Init(loop *Loop) int {
	return int(libuv.FsEventInit(loop.Loop, e.FsEvent))
}

// Start listening for file events
func (e *FsEvent) Start(cb FsEventCb, path string, flags int) int {
	e.FsEventCb = cb
	return int(e.FsEvent.Start(func(_handle *libuv.FsEvent, filename *c.Char, events c.Int, status c.Int) {
		fsEvent := (*FsEvent)(unsafe.Pointer(_handle))
		fsEvent.FsEventCb(fsEvent, filename, events, status)
	}, c.AllocaCStr(path), c.Int(flags)))
}

// Stop listening for file events
func (e *FsEvent) Stop() int {
	return int(e.FsEvent.Stop())
}

// Close the file event handle
func (e *FsEvent) Close() int {
	return int(e.FsEvent.Close())
}

// GetPath Get the path of the file event
func (e *FsEvent) GetPath() string {
	return c.GoString(e.FsEvent.Getpath())
}

// Init Initialize a file poll handle
func (p *FsPoll) Init(loop *Loop) int {
	return int(libuv.FsPollInit(loop.Loop, p.FsPoll))
}

// Start polling for file changes
func (p *FsPoll) Start(cb FsPollCb, path string, interval uint) int {
	p.FsPollCb = cb
	return int(p.FsPoll.Start(func(_handle *libuv.FsPoll, status c.Int, events c.Int) {
		fsPoll := (*FsPoll)(unsafe.Pointer(_handle))
		fsPoll.FsPollCb(fsPoll, status, events)
	}, c.AllocaCStr(path), interval))
}

// Stop polling for file changes
func (p *FsPoll) Stop() int {
	return int(p.FsPoll.Stop())
}

// Close the file poll handle
func (p *FsPoll) Close() int {
	return int(p.FsPoll.Close())
}

// GetPath Get the path of the file poll
func (p *FsPoll) GetPath() string {
	return c.GoString(p.FsPoll.GetPath())
}

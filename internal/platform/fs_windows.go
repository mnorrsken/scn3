package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileIDBothDirInfo mirrors FILE_ID_BOTH_DIR_INFO. FileName runs past the struct.
type fileIDBothDirInfo struct {
	NextEntryOffset uint32
	FileIndex       uint32
	CreationTime    int64
	LastAccessTime  int64
	LastWriteTime   int64
	ChangeTime      int64
	EndOfFile       int64
	AllocationSize  int64
	FileAttributes  uint32
	FileNameLength  uint32
	EaSize          uint32
	ShortNameLength uint8
	_               uint8
	ShortName       [12]uint16
	_               uint16
	FileID          int64
	FileName        [1]uint16
}

// longPath adds the \\?\ prefix that long absolute paths need.
func longPath(path string) string {
	if len(path) < 240 || strings.HasPrefix(path, `\\`) {
		return path
	}
	return `\\?\` + path
}

func openDir(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
}

// fileFullDirInfo mirrors FILE_FULL_DIR_INFO: the same as above without file IDs.
type fileFullDirInfo struct {
	NextEntryOffset uint32
	FileIndex       uint32
	CreationTime    int64
	LastAccessTime  int64
	LastWriteTime   int64
	ChangeTime      int64
	EndOfFile       int64
	AllocationSize  int64
	FileAttributes  uint32
	FileNameLength  uint32
	EaSize          uint32
	FileName        [1]uint16
}

// filetimeToUnix converts 100 ns ticks since 1601 to Unix seconds.
func filetimeToUnix(t int64) int64 {
	return (t - 116444736000000000) / 10000000
}

// allowFileIDs lets tests force the listing without file IDs.
var allowFileIDs = true

// ReadDir lists path with GetFileInformationByHandleEx. One call returns a
// whole batch with allocation size (right for compressed and sparse files),
// so no file has to be opened one by one. Reparse points (junctions, symlinks,
// OneDrive placeholders) are never entered.
//
// It follows what Go's os.ReadDir does: ask for file IDs only when the volume
// says it supports them, start with the "restart" class, and treat
// ERROR_FILE_NOT_FOUND on the first call as an empty directory. If the call
// with file IDs fails anyway, it lists again without them. Then hard links
// cannot be recognised and count once per link.
func ReadDir(path string) ([]Entry, error) {
	h, err := openDir(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var serial, flags uint32
	if err := windows.GetVolumeInformationByHandle(h, nil, 0, &serial, nil, &flags, nil, 0); err != nil {
		flags = 0
	}
	withIDs := allowFileIDs &&
		flags&windows.FILE_SUPPORTS_OBJECT_IDS != 0 && flags&windows.FILE_SUPPORTS_OPEN_BY_FILE_ID != 0

	var out []Entry
	// The records hold 64-bit fields, and Windows fails with ERROR_NOACCESS (998)
	// if the buffer is not 8-byte aligned. A []byte only guarantees 1 byte, and
	// this one stays on the goroutine stack, so build it from uint64s.
	words := make([]uint64, 8*1024)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*8)
	first := true
	for {
		class := uint32(windows.FileFullDirectoryInfo)
		switch {
		case withIDs && first:
			class = windows.FileIdBothDirectoryRestartInfo
		case withIDs:
			class = windows.FileIdBothDirectoryInfo
		case first:
			class = windows.FileFullDirectoryRestartInfo
		}
		err := windows.GetFileInformationByHandleEx(h, class, &buf[0], uint32(len(buf)))
		switch {
		case errors.Is(err, windows.ERROR_NO_MORE_FILES):
			return out, nil
		case first && errors.Is(err, windows.ERROR_FILE_NOT_FOUND):
			return out, nil
		case first && withIDs && err != nil:
			withIDs = false
			continue
		case err != nil:
			return out, fmt.Errorf("listing (class %d): %w", class, err)
		}
		first = false
		for off := uintptr(0); ; {
			var e Entry
			var attrs, next uint32
			if withIDs {
				info := (*fileIDBothDirInfo)(unsafe.Pointer(&buf[off]))
				e.Name = windows.UTF16ToString(unsafe.Slice(&info.FileName[0], info.FileNameLength/2))
				e.Size, e.Ino, attrs, next = info.AllocationSize, uint64(info.FileID), info.FileAttributes, info.NextEntryOffset
				e.Mtime = filetimeToUnix(info.LastWriteTime)
			} else {
				info := (*fileFullDirInfo)(unsafe.Pointer(&buf[off]))
				e.Name = windows.UTF16ToString(unsafe.Slice(&info.FileName[0], info.FileNameLength/2))
				e.Size, attrs, next = info.AllocationSize, info.FileAttributes, info.NextEntryOffset
				e.Mtime = filetimeToUnix(info.LastWriteTime)
			}
			if e.Name != "." && e.Name != ".." {
				e.IsDir = attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0
				e.Dev = uint64(serial)
				e.Linked = withIDs && attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0
				out = append(out, e)
			}
			if next == 0 {
				break
			}
			off += uintptr(next)
		}
	}
}

type windowsPathError struct {
	path string
	err  error
}

func (e *windowsPathError) Error() string { return e.path + ": " + e.err.Error() }
func (e *windowsPathError) Unwrap() error { return e.err }

// Stat describes the directory at path.
func Stat(path string) (Entry, error) {
	h, err := openDir(path)
	if err != nil {
		return Entry{}, &windowsPathError{path, err}
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var fi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &fi); err != nil {
		return Entry{}, &windowsPathError{path, err}
	}
	return Entry{
		Name:  path,
		IsDir: fi.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0,
		Dev:   uint64(fi.VolumeSerialNumber),
		Ino:   uint64(fi.FileIndexHigh)<<32 | uint64(fi.FileIndexLow),
		Mtime: fi.LastWriteTime.Nanoseconds() / 1e9,
	}, nil
}

// Usage returns the capacity and free bytes of the volume holding path.
func Usage(path string) (capacity, free int64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &totalFree); err != nil {
		return 0, 0, fmt.Errorf("free space of %s: %w", path, err)
	}
	return int64(total), int64(avail), nil
}

// Volumes lists drives that have media.
func Volumes() ([]Volume, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, fmt.Errorf("listing drives: %w", err)
	}
	var out []Volume
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		capacity, free, err := Usage(root)
		if err != nil || capacity == 0 {
			continue // empty card reader, disconnected network drive
		}
		p, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		var label, fs [windows.MAX_PATH + 1]uint16
		name := root[:2]
		fsName := ""
		if windows.GetVolumeInformation(p, &label[0], uint32(len(label)), nil, nil, nil, &fs[0], uint32(len(fs))) == nil {
			fsName = windows.UTF16ToString(fs[:])
			if l := windows.UTF16ToString(label[:]); l != "" {
				name = l + " (" + root[:2] + ")"
			}
		}
		kind := driveKind(root)
		out = append(out, Volume{Path: root, Name: name, FS: fsName, Kind: kind, Hidden: kind == KindImage, Capacity: capacity, Free: free})
	}
	return out, nil
}

// driveKind tells what kind of drive root (like `C:\`) is. Windows reports USB
// hard disks as fixed, so for those the bus type decides.
func driveKind(root string) string {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return KindInternal
	}
	switch windows.GetDriveType(p) {
	case windows.DRIVE_REMOTE:
		return KindNetwork
	case windows.DRIVE_CDROM:
		return KindOptical
	case windows.DRIVE_REMOVABLE:
		return KindExternal
	}
	if strings.EqualFold(root[:2], os.Getenv("SystemDrive")) {
		return KindSystem
	}
	switch busType(root[:2]) {
	case busUSB, bus1394, busSD, busMMC:
		return KindExternal
	case busVirtual, busFileBackedVirtual:
		return KindImage
	}
	return KindInternal
}

const (
	ioctlStorageQueryProperty = 0x2D1400
	bus1394                   = 4
	busUSB                    = 7
	busSD                     = 0xC
	busMMC                    = 0xD
	busVirtual                = 0xE
	busFileBackedVirtual      = 0xF
)

// busType asks the volume's device how it is attached (STORAGE_BUS_TYPE), or -1.
func busType(drive string) int {
	p, err := windows.UTF16PtrFromString(`\\.\` + drive)
	if err != nil {
		return -1
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return -1
	}
	defer func() { _ = windows.CloseHandle(h) }()
	// STORAGE_PROPERTY_QUERY{StorageDeviceProperty, PropertyStandardQuery}.
	query := [3]uint32{}
	// STORAGE_DEVICE_DESCRIPTOR: BusType is the uint32 at offset 28.
	var out [64]uint32
	var n uint32
	err = windows.DeviceIoControl(h, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&query[0])), uint32(unsafe.Sizeof(query)),
		(*byte)(unsafe.Pointer(&out[0])), uint32(unsafe.Sizeof(out)), &n, nil)
	if err != nil || n < 32 {
		return -1
	}
	return int(out[7])
}

// NewBoundary allows everything: Windows mounts and links are reparse points,
// which ReadDir already refuses to enter.
func NewBoundary(Entry, string) *Boundary {
	return &Boundary{}
}

// shFileOpStruct mirrors SHFILEOPSTRUCTW.
type shFileOpStruct struct {
	hwnd      uintptr
	wFunc     uint32
	pFrom     *uint16
	pTo       *uint16
	fFlags    uint16
	aborted   int32
	nameMaps  uintptr
	progTitle *uint16
}

const (
	foDelete           = 3
	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000
)

var shFileOperation = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// Trash moves path to the Recycle Bin. If an item cannot be recycled (some
// network shares, items too big for the bin), Windows asks before destroying it.
func Trash(path string) error {
	// pFrom is a list that ends in a double NUL.
	from, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0)
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofWantNukeWarning,
	}
	r, _, _ := shFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return fmt.Errorf("recycling %s: shell error 0x%x", path, r)
	}
	if op.aborted != 0 {
		return fmt.Errorf("recycling %s: cancelled", path)
	}
	return nil
}

// Reveal selects path in Explorer.
func Reveal(path string) error {
	cmd := exec.Command("explorer")
	// Explorer wants the quotes after the comma, which os/exec would not produce.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + filepath.Clean(path) + `"`}
	return cmd.Start()
}

// RevealDir opens path itself in Explorer.
func RevealDir(path string) error {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer "` + filepath.Clean(path) + `"`}
	return cmd.Start()
}

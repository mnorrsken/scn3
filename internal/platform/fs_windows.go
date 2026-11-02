package platform

import (
	"errors"
	"fmt"
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

// ReadDir lists path through FileIdBothDirectoryInfo. One call returns a whole
// batch with allocation size (right for compressed and sparse files) and file
// ID, so no file has to be opened one by one. Reparse points (junctions,
// symlinks, OneDrive placeholders) are never entered.
func ReadDir(path string) ([]Entry, error) {
	h, err := openDir(path)
	if err != nil {
		return nil, &windowsPathError{path, err}
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var vol windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &vol); err != nil {
		return nil, &windowsPathError{path, err}
	}

	var out []Entry
	buf := make([]byte, 64*1024)
	for {
		err := windows.GetFileInformationByHandleEx(h, windows.FileIdBothDirectoryInfo, &buf[0], uint32(len(buf)))
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return out, nil
		}
		if err != nil {
			return out, &windowsPathError{path, err}
		}
		for off := uintptr(0); ; {
			info := (*fileIDBothDirInfo)(unsafe.Pointer(&buf[off]))
			name := windows.UTF16ToString(unsafe.Slice(&info.FileName[0], info.FileNameLength/2))
			if name != "." && name != ".." {
				attrs := info.FileAttributes
				isDir := attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0
				out = append(out, Entry{
					Name:   name,
					IsDir:  isDir,
					Size:   info.AllocationSize,
					Dev:    uint64(vol.VolumeSerialNumber),
					Ino:    uint64(info.FileID),
					Linked: attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0,
				})
			}
			if info.NextEntryOffset == 0 {
				break
			}
			off += uintptr(info.NextEntryOffset)
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
		out = append(out, Volume{Path: root, Name: name, FS: fsName, Capacity: capacity, Free: free})
	}
	return out, nil
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

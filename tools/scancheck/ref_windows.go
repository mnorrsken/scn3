package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	attrRecallOnOpen = 0x40000
	tagNameSurrogate = 0x20000000
	fileAttrTagInfo  = 9 // FileAttributeTagInfo
)

var getCompressedFileSize = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCompressedFileSizeW")

// inspect reads one listed item. It opens each file to learn its hard-link
// count and file ID, which the app's fast listing does not ask for; slow,
// which is fine for a check.
func inspect(path string, _ os.DirEntry) (entryInfo, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return entryInfo{}, err
	}
	data, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return entryInfo{}, fmt.Errorf("no attribute data")
	}
	attrs := data.FileAttributes
	info := entryInfo{isDir: attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0, links: 1}

	h, herr := open(path)
	if herr == nil {
		defer func() { _ = windows.CloseHandle(h) }()
	}
	if info.isDir {
		if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			tag := reparseTag(h, herr)
			switch {
			case tag&tagNameSurrogate != 0:
				info.follow, info.detail = reasonLink, fmt.Sprintf("reparse tag 0x%08X", tag)
			case attrs&attrRecallOnOpen != 0:
				info.follow, info.detail = reasonOnlineOnly, fmt.Sprintf("reparse tag 0x%08X", tag)
			}
		}
		return info, nil
	}
	info.size = compressedSize(path, fi.Size())
	if herr == nil {
		var bh windows.ByHandleFileInformation
		if windows.GetFileInformationByHandle(h, &bh) == nil {
			info.links = bh.NumberOfLinks
			info.dev = uint64(bh.VolumeSerialNumber)
			info.key = fileKey{info.dev, uint64(bh.FileIndexHigh)<<32 | uint64(bh.FileIndexLow)}
		}
	}
	return info, nil
}

// open opens an item for reading its attributes only, without following a
// reparse point and without making a cloud file download.
func open(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
}

func reparseTag(h windows.Handle, openErr error) uint32 {
	if openErr != nil {
		return 0
	}
	var info struct{ FileAttributes, ReparseTag uint32 }
	if windows.GetFileInformationByHandleEx(h, fileAttrTagInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))) != nil {
		return 0
	}
	return info.ReparseTag
}

// compressedSize is the bytes on disk, which differ from the length for
// compressed, sparse and online-only files.
func compressedSize(path string, length int64) int64 {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return length
	}
	var high uint32
	low, _, callErr := getCompressedFileSize.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&high)))
	if uint32(low) == 0xFFFFFFFF && callErr != windows.ERROR_SUCCESS {
		return length
	}
	return int64(high)<<32 | int64(uint32(low))
}

func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

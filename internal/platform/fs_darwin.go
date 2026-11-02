package platform

import (
	"encoding/binary"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

const dataVolume = "/System/Volumes/Data"

const (
	attrCloneID      = 0x100 // ATTR_CMNEXT_CLONEID
	attrExtFlags     = 0x200 // ATTR_CMNEXT_EXT_FLAGS
	efMayShareBlocks = 1     // EF_MAY_SHARE_BLOCKS

	// cloneDevBit keeps clone IDs apart from inode numbers in the (Dev, Ino)
	// table: they are separate number ranges that could overlap.
	cloneDevBit = 1 << 63
)

// markClone makes APFS clones of one file count once. A clone has its own inode
// and full st_blocks while sharing its blocks, so without this every copy counts
// in full. Pure clones share a clone ID, which then becomes the file's identity.
// A clone that was modified afterwards gets a new ID and still counts in full.
// Other filesystems fail this call or leave the flags out: such files stay as they are.
func markClone(dirfd int, name string, e *Entry) {
	path, err := unix.BytePtrFromString(name)
	if err != nil {
		return
	}
	list := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Forkattr: attrCloneID | attrExtFlags}
	// The result is packed: a uint32 length, then the two uint64 values.
	var buf [20]byte
	// x/sys has no wrapper for this call. If it ever stops working, clones just count in full again.
	_, _, errno := unix.Syscall6(unix.SYS_GETATTRLISTAT, uintptr(dirfd), uintptr(unsafe.Pointer(path)), //nolint:staticcheck
		uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&buf)), uintptr(len(buf)),
		unix.FSOPT_NOFOLLOW|unix.FSOPT_ATTR_CMN_EXTENDED)
	if errno != 0 || binary.LittleEndian.Uint32(buf[:4]) != uint32(len(buf)) {
		return
	}
	if binary.LittleEndian.Uint64(buf[12:20])&efMayShareBlocks == 0 {
		return
	}
	e.Linked = true
	e.Dev |= cloneDevBit
	e.Ino = binary.LittleEndian.Uint64(buf[4:12])
}

// Volumes lists mounted filesystems, without system snapshot volumes.
func Volumes() ([]Volume, error) {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, fmt.Errorf("getfsstat: %w", err)
	}
	buf := make([]unix.Statfs_t, n)
	n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return nil, fmt.Errorf("getfsstat: %w", err)
	}
	var out []Volume
	for _, st := range buf[:n] {
		path := unix.ByteSliceToString(byteSlice(st.Mntonname[:]))
		fs := unix.ByteSliceToString(byteSlice(st.Fstypename[:]))
		if fs == "devfs" || fs == "autofs" || st.Blocks == 0 ||
			strings.HasPrefix(path, "/System/Volumes/") || strings.HasPrefix(path, "/Library/Developer/CoreSimulator/") {
			continue
		}
		name := filepath.Base(path)
		if path == "/" {
			name = "Macintosh HD"
		}
		bsize := int64(st.Bsize)
		out = append(out, Volume{
			Path:     path,
			Name:     name,
			FS:       fs,
			Kind:     volumeKind(path, st.Flags),
			Capacity: int64(st.Blocks) * bsize,
			Free:     int64(st.Bavail) * bsize,
		})
	}
	return out, nil
}

// volumeKind tells what kind of drive path is on. The mount flags settle the
// system drive and network shares; the rest needs Disk Arbitration (kind_darwin.go).
func volumeKind(path string, flags uint32) string {
	switch {
	case flags&unix.MNT_ROOTFS != 0:
		return KindSystem
	case flags&unix.MNT_LOCAL == 0:
		return KindNetwork
	}
	if v, ok := kindCache.Load(path); ok {
		return v.(string)
	}
	kind := diskKind(path)
	kindCache.Store(path, kind)
	return kind
}

// kindCache holds Disk Arbitration answers: Volumes runs often, a mount rarely changes.
var kindCache sync.Map

func byteSlice[T byte | int8](s []T) []byte {
	b := make([]byte, len(s))
	for i, c := range s {
		b[i] = byte(c)
	}
	return b
}

// NewBoundary keeps a scan on the device of root. "/" is a special case: the
// system volume reaches user data on the Data volume through firmlinks such as
// /Users, so that device is allowed too and the direct path to it is skipped,
// or everything on it would be counted twice.
func NewBoundary(root Entry, rootPath string) *Boundary {
	b := &Boundary{devs: map[uint64]struct{}{root.Dev: {}}}
	if filepath.Clean(rootPath) == "/" {
		b.skip = map[string]struct{}{dataVolume: {}}
		if data, err := Stat(dataVolume); err == nil {
			b.devs[data.Dev] = struct{}{}
		}
	}
	return b
}

// Reveal selects path in Finder.
func Reveal(path string) error {
	return exec.Command("open", "-R", path).Start()
}

// RevealDir opens path itself in Finder.
func RevealDir(path string) error {
	return exec.Command("open", path).Start()
}

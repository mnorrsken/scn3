// Package platform holds everything that differs per OS: volume listing, size
// on disk, what not to follow, trash and the file manager. It has no UI imports.
package platform

import "path/filepath"

// Kinds of volumes, for the icon next to each.
const (
	KindSystem   = "system"   // the drive the OS runs from
	KindInternal = "internal" // another built-in disk
	KindExternal = "external" // USB, SD card, other removable media
	KindNetwork  = "network"  // SMB, NFS and other network shares
	KindOptical  = "optical"  // CD, DVD, Blu-ray
	KindImage    = "image"    // disk image, loop or virtual disk
)

// Volume is a mounted filesystem.
type Volume struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	FS       string `json:"fs"`
	Kind     string `json:"kind"`
	Hidden   bool   `json:"hidden"` // not a drive people think of: disk images, system and container mounts
	Capacity int64  `json:"capacity"`
	Free     int64  `json:"free"`
}

// Entry is one item of a directory.
type Entry struct {
	Name   string
	IsDir  bool   // a real directory, safe to descend into if Boundary agrees
	Size   int64  // bytes on disk
	Dev    uint64 // device or volume number
	Ino    uint64 // inode or file ID
	Linked bool   // may share its blocks with another file (hard link, clone), so count once per (Dev, Ino)
}

// Boundary decides which subdirectories a scan may enter.
type Boundary struct {
	devs map[uint64]struct{}
	skip map[string]struct{}
}

// Follow reports whether a scan may enter the directory e at path.
func (b *Boundary) Follow(e Entry, path string) bool {
	if _, skip := b.skip[filepath.Clean(path)]; skip {
		return false
	}
	if b.devs == nil {
		return true
	}
	_, ok := b.devs[e.Dev]
	return ok
}

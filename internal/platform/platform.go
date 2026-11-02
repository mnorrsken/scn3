// Package platform holds everything that differs per OS: volume listing, size
// on disk, what not to follow, trash and the file manager. It has no UI imports.
package platform

import "path/filepath"

// Volume is a mounted filesystem.
type Volume struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	FS       string `json:"fs"`
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

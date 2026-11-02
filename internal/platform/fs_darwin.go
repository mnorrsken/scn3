package platform

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const dataVolume = "/System/Volumes/Data"

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
			Capacity: int64(st.Blocks) * bsize,
			Free:     int64(st.Bavail) * bsize,
		})
	}
	return out, nil
}

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

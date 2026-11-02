//go:build linux || darwin

package platform

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// ReadDir lists path without following links. On a partial failure it returns
// the entries it could read together with the error.
func ReadDir(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	entries := make([]Entry, 0, len(names))
	fd := int(f.Fd())
	var firstErr = err
	for _, name := range names {
		var st unix.Stat_t
		if err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			// Deleted since the listing: not an error worth reporting.
			if !errors.Is(err, unix.ENOENT) && firstErr == nil {
				firstErr = fmt.Errorf("stat %s: %w", name, err)
			}
			continue
		}
		e := entryOf(name, &st)
		if st.Mode&unix.S_IFMT == unix.S_IFREG {
			markClone(fd, name, &e)
		}
		entries = append(entries, e)
	}
	return entries, firstErr
}

// Stat describes path itself, following a link only if path is one.
func Stat(path string) (Entry, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return Entry{}, err
	}
	return entryOf(path, &st), nil
}

func entryOf(name string, st *unix.Stat_t) Entry {
	isDir := uint32(st.Mode)&unix.S_IFMT == unix.S_IFDIR
	return Entry{
		Name:   name,
		IsDir:  isDir,
		Size:   st.Blocks * 512,
		Dev:    uint64(st.Dev),
		Ino:    st.Ino,
		Linked: !isDir && st.Nlink > 1,
	}
}

// Usage returns the capacity and free bytes of the filesystem holding path.
func Usage(path string) (capacity, free int64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	bsize := int64(st.Bsize)
	return int64(st.Blocks) * bsize, int64(st.Bavail) * bsize, nil
}

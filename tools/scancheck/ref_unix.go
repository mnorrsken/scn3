//go:build linux || darwin

package main

import (
	"errors"
	"os"
	"syscall"
)

// inspect reads one listed item with lstat, so symlinks are not followed.
func inspect(path string, _ os.DirEntry) (entryInfo, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return entryInfo{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return entryInfo{}, errors.New("no stat data")
	}
	dev := uint64(st.Dev) //nolint:unconvert // int32 on macOS, uint64 on Linux
	return entryInfo{
		isDir: fi.IsDir(),
		size:  st.Blocks * 512,
		dev:   dev,
		key:   fileKey{dev, st.Ino},
		links: uint32(st.Nlink),
	}, nil
}

func elevated() bool { return os.Geteuid() == 0 }

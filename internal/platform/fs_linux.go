package platform

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var pseudoFS = map[string]bool{
	"autofs": true, "binfmt_misc": true, "bpf": true, "cgroup": true, "cgroup2": true,
	"configfs": true, "debugfs": true, "devpts": true, "devtmpfs": true, "efivarfs": true,
	"fuse.gvfsd-fuse": true, "fuse.portal": true, "fusectl": true, "hugetlbfs": true,
	"mqueue": true, "nsfs": true, "overlay": true, "proc": true, "pstore": true,
	"ramfs": true, "rpc_pipefs": true, "securityfs": true, "selinuxfs": true,
	"squashfs": true, "sysfs": true, "tmpfs": true, "tracefs": true,
}

type mount struct {
	id, path, fs string
}

// parseMountInfo reads /proc/self/mountinfo. Only the first mount of each
// device is kept, so bind mounts and btrfs subvolumes of one device show once.
func parseMountInfo(r io.Reader) ([]mount, error) {
	var out []mount
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		sep := -1
		for i, s := range f {
			if s == "-" {
				sep = i
				break
			}
		}
		if len(f) < 5 || sep < 0 || sep+1 >= len(f) {
			continue
		}
		m := mount{id: f[2], path: unescapeMount(f[4]), fs: f[sep+1]}
		if pseudoFS[m.fs] || seen[m.id] {
			continue
		}
		seen[m.id] = true
		out = append(out, m)
	}
	return out, sc.Err()
}

// unescapeMount decodes the \040-style octal escapes of mountinfo.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Volumes lists real mounted filesystems.
func Volumes() ([]Volume, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("reading mounts: %w", err)
	}
	defer f.Close()
	mounts, err := parseMountInfo(f)
	if err != nil {
		return nil, fmt.Errorf("reading mounts: %w", err)
	}
	var out []Volume
	for _, m := range mounts {
		capacity, free, err := Usage(m.path)
		if err != nil || capacity == 0 {
			continue
		}
		out = append(out, Volume{Path: m.path, Name: m.path, FS: m.fs, Capacity: capacity, Free: free})
	}
	return out, nil
}

// NewBoundary keeps a scan on the device of root and out of /proc.
func NewBoundary(root Entry, _ string) *Boundary {
	return &Boundary{
		devs: map[uint64]struct{}{root.Dev: {}},
		skip: map[string]struct{}{"/proc": {}},
	}
}

// Trash moves path to the freedesktop.org trash of its filesystem. It never
// deletes: if no trash directory can take the item it returns an error.
func Trash(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}
	item, err := Stat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("trash %s: %w", path, err)
	}
	var errs []string
	for _, dir := range trashDirs(path, item.Dev) {
		err := trashInto(dir, path)
		if err == nil {
			return nil
		}
		errs = append(errs, err.Error())
	}
	return fmt.Errorf("trash %s: no usable trash directory: %s", path, strings.Join(errs, "; "))
}

// trashDirs returns the trash directories on the device dev, best first.
func trashDirs(path string, dev uint64) []string {
	var dirs []string
	data := os.Getenv("XDG_DATA_HOME")
	if home, err := os.UserHomeDir(); data == "" && err == nil {
		data = filepath.Join(home, ".local", "share")
	}
	if data != "" {
		// The home trash may not exist yet, so compare the nearest existing parent.
		for p := data; ; p = filepath.Dir(p) {
			if e, err := Stat(p); err == nil {
				if e.Dev == dev {
					dirs = append(dirs, filepath.Join(data, "Trash"))
				}
				break
			}
			if p == filepath.Dir(p) {
				break
			}
		}
	}
	// $topdir/.Trash-$uid on the same mount.
	top := filepath.Dir(path)
	for {
		parent := filepath.Dir(top)
		e, err := Stat(parent)
		if parent == top || err != nil || e.Dev != dev {
			break
		}
		top = parent
	}
	return append(dirs, filepath.Join(top, ".Trash-"+strconv.Itoa(os.Getuid())))
}

func trashInto(trash, path string) error {
	files, infos := filepath.Join(trash, "files"), filepath.Join(trash, "info")
	for _, d := range []string{files, infos} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	base := filepath.Base(path)
	for i := 0; i < 10000; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s.%d", base, i)
		}
		// Claiming the info file with O_EXCL reserves the name, as the spec asks.
		infoPath := filepath.Join(infos, name+".trashinfo")
		info, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		_, werr := fmt.Fprintf(info, "[Trash Info]\nPath=%s\nDeletionDate=%s\n",
			escapeTrashPath(path), time.Now().Format("2006-01-02T15:04:05"))
		if cerr := info.Close(); werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Rename(path, filepath.Join(files, name))
		}
		if werr != nil {
			os.Remove(infoPath)
		}
		return werr
	}
	return fmt.Errorf("too many items named %s in %s", base, trash)
}

func escapeTrashPath(p string) string {
	var b bytes.Buffer
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '/' || c == '-' || c == '_' || c == '.' || c == '~' ||
			'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// Reveal opens the parent of path in the file manager.
func Reveal(path string) error {
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}

// RevealDir opens path itself in the file manager.
func RevealDir(path string) error {
	return exec.Command("xdg-open", path).Start()
}

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMountInfo(t *testing.T) {
	const in = `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
23 22 0:5 / /proc rw - proc proc rw
24 22 8:17 / /mnt/my\040disk rw - xfs /dev/sdb1 rw
25 22 8:1 /sub /bind rw - ext4 /dev/sda1 rw
26 22 0:30 / /run rw shared:5 master:2 - tmpfs tmpfs rw
`
	got, err := parseMountInfo(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].path != "/" || got[1].path != "/mnt/my disk" || got[1].fs != "xfs" {
		t.Fatalf("got %+v", got)
	}
}

func TestMountPointsBelowRootAreSkipped(t *testing.T) {
	const in = `22 1 8:1 / / rw - ext4 /dev/sda1 rw
23 22 0:5 / /proc rw - proc proc rw
24 22 8:1 /data /srv/bind rw - ext4 /dev/sda1 rw
25 22 0:40 / /home rw - btrfs /dev/sdb1 rw
26 25 0:41 / /home/me/snap rw - btrfs /dev/sdb1 rw
`
	points, err := mountPoints(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if got := skipMounts(points, "/"); strings.Join(got, ",") != "/proc,/srv/bind,/home,/home/me/snap" {
		t.Fatalf("from /: %v", got)
	}
	if got := skipMounts(points, "/home"); strings.Join(got, ",") != "/home/me/snap" {
		t.Fatalf("from /home: %v", got)
	}
	if got := skipMounts(points, "/ho"); len(got) != 0 {
		t.Fatalf("from /ho: %v", got)
	}
}

func TestVolumeKind(t *testing.T) {
	sys := t.TempDir()
	mk := func(dev string, files map[string]string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(sys, dev), 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(sys, dev, name), []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	link := func(id, dev string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(sys, "dev/block"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(sys, dev), filepath.Join(sys, "dev/block", id)); err != nil {
			t.Fatal(err)
		}
	}
	mk("devices/pci0/ata1/block/sda", map[string]string{"removable": "0\n"})
	mk("devices/pci0/ata1/block/sda/sda2", map[string]string{"partition": "2\n"})
	link("8:2", "devices/pci0/ata1/block/sda/sda2")
	mk("devices/pci0/usb2/2-1/host6/block/sdb", map[string]string{"removable": "0\n"})
	mk("devices/pci0/usb2/2-1/host6/block/sdb/sdb1", map[string]string{"partition": "1\n"})
	link("8:17", "devices/pci0/usb2/2-1/host6/block/sdb/sdb1")
	mk("devices/pci0/mmc0/block/mmcblk0", map[string]string{"removable": "1\n"})
	link("179:0", "devices/pci0/mmc0/block/mmcblk0")
	mk("devices/virtual/block/loop3", map[string]string{"removable": "0\n"})
	link("7:3", "devices/virtual/block/loop3")
	mk("devices/pci0/ata2/block/sr0", map[string]string{"removable": "1\n"})
	link("11:0", "devices/pci0/ata2/block/sr0")

	for _, c := range []struct {
		m    mount
		want string
	}{
		{mount{"8:2", "/", "ext4"}, KindSystem},
		{mount{"8:2", "/data", "ext4"}, KindInternal},
		{mount{"8:17", "/media/usb", "exfat"}, KindExternal},
		{mount{"179:0", "/media/sd", "vfat"}, KindExternal},
		{mount{"7:3", "/snap/core/1", "ext4"}, KindImage},
		{mount{"11:0", "/media/cdrom", "iso9660"}, KindOptical},
		{mount{"0:55", "/mnt/nas", "cifs"}, KindNetwork},
		{mount{"0:56", "/mnt/other", "zfs"}, KindInternal},
	} {
		if got := volumeKind(c.m, sys); got != c.want {
			t.Errorf("%s: got %s, want %s", c.m.path, got, c.want)
		}
	}
}

func TestHiddenMounts(t *testing.T) {
	for _, c := range []struct {
		m      mount
		kind   string
		hidden bool
	}{
		{mount{"8:2", "/", "ext4"}, KindSystem, false},
		{mount{"0:40", "/home", "btrfs"}, KindInternal, false},
		{mount{"8:17", "/media/me/usb", "exfat"}, KindExternal, false},
		{mount{"0:55", "/mnt/nas", "cifs"}, KindNetwork, false},
		{mount{"0:56", "/mnt/nas2", "fuse.sshfs"}, KindNetwork, false},
		{mount{"8:1", "/boot/efi", "vfat"}, KindInternal, true},
		{mount{"8:3", "/boot", "ext4"}, KindInternal, true},
		{mount{"8:4", "/bootstrap", "ext4"}, KindInternal, false},
		{mount{"7:3", "/media/iso", "iso9660"}, KindImage, true},
		{mount{"0:60", "/var/lib/docker/volumes/x", "ext4"}, KindInternal, true},
		{mount{"0:61", "/run/user/1000/doc", "fuse.portal"}, KindInternal, true},
	} {
		if got := hiddenMount(c.m, c.kind); got != c.hidden {
			t.Errorf("%s: hidden = %v, want %v", c.m.path, got, c.hidden)
		}
	}
}

func TestTrashFollowsSpec(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	victim := filepath.Join(base, "my dir")
	if err := os.Mkdir(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if i == 1 {
			if err := os.Mkdir(victim, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := Trash(victim); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(victim); !os.IsNotExist(err) {
			t.Fatal("item still in place")
		}
	}
	trash := filepath.Join(base, "data", "Trash")
	for _, p := range []string{"files/my dir", "files/my dir.1"} {
		if _, err := os.Stat(filepath.Join(trash, p)); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.ReadFile(filepath.Join(trash, "info", "my dir.trashinfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(info), "/my%20dir\n") || !strings.HasPrefix(string(info), "[Trash Info]\nPath=/") {
		t.Fatalf("trashinfo:\n%s", info)
	}
}

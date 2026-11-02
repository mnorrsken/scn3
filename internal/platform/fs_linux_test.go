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

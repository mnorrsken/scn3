package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDirReportsSizesAndIDs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), make([]byte, 100_000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	for _, e := range entries {
		switch e.Name {
		case "f":
			if e.IsDir || !e.Linked || e.Ino == 0 || e.Size < 100_000 {
				t.Fatalf("file = %+v", e)
			}
		case "d":
			if !e.IsDir {
				t.Fatalf("dir = %+v", e)
			}
		default:
			t.Fatalf("unexpected %q", e.Name)
		}
	}
}

func TestVolumesFindsSystemDrive(t *testing.T) {
	vols, err := Volumes()
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) == 0 || vols[0].Capacity == 0 {
		t.Fatalf("volumes = %+v", vols)
	}
}

func TestTrashRemovesFolder(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.Mkdir(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Trash(victim); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Fatalf("still there: %v", err)
	}
}

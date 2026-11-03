package platform

import (
	"os"
	"os/exec"
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

func TestReadDirWithoutFileIDs(t *testing.T) {
	allowFileIDs = false
	t.Cleanup(func() { allowFileIDs = true })
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
		if e.Name == "f" && (e.IsDir || e.Linked || e.Size < 100_000) {
			t.Fatalf("file = %+v", e)
		}
		if e.Name == "d" && !e.IsDir {
			t.Fatalf("dir = %+v", e)
		}
	}
}

func TestReadDirEmptyAndVolumeRoot(t *testing.T) {
	entries, err := ReadDir(t.TempDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty dir: %v %+v", err, entries)
	}
	root := os.Getenv("SystemDrive") + `\`
	entries, err = ReadDir(root)
	if err != nil || len(entries) == 0 {
		t.Fatalf("%s: %v, %d entries", root, err, len(entries))
	}
}

func TestSystemDriveKind(t *testing.T) {
	root := os.Getenv("SystemDrive") + `\`
	if got := driveKind(root); got != KindSystem {
		t.Fatalf("%s is %s", root, got)
	}
}

func TestJunctionIsNotEntered(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	// mklink /J makes a junction without admin rights.
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot make a junction: %v %s", err, out)
	}
	entries, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "link" && e.IsDir {
			t.Fatal("junction would be entered")
		}
		if e.Name == "target" && !e.IsDir {
			t.Fatal("real folder would not be entered")
		}
	}
}

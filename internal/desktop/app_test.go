package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mnorrsken/scn3/internal/platform"
	"github.com/mnorrsken/scn3/internal/tree"
)

func waitIdle(t *testing.T, a *App, ref int64) {
	t.Helper()
	s, _, err := a.lookup(ref)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		a.mu.Lock()
		running := s.running
		a.mu.Unlock()
		if !running {
			return
		}
	}
	t.Fatal("scan did not finish")
}

func setup(t *testing.T) (*App, int64, string) {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{"big/a", "big/sub/b", "small/c"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, 200_000), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := New(func(string, any) {})
	ref, err := a.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a, ref)
	return a, ref, root
}

func find(t *testing.T, v *View, name string) *tree.ViewNode {
	t.Helper()
	for _, k := range v.Root.Kids {
		if k.Name == name {
			return k
		}
	}
	return nil
}

func TestRefRoundTrip(t *testing.T) {
	idx, id := decode(encode(7, 123))
	if idx != 7 || id != 123 {
		t.Fatalf("got %d %d", idx, id)
	}
}

func TestViewHasRefsThatResolve(t *testing.T) {
	a, ref, _ := setup(t)
	v, err := a.View(ref, 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	big := find(t, v, "big")
	if big == nil {
		t.Fatal("no big")
	}
	if v.Parent != -1 {
		t.Fatalf("root parent = %d", v.Parent)
	}
	sub, err := a.View(big.Ref, 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Parent != ref || len(sub.Crumbs) != 2 || sub.Crumbs[1].Name != "big" {
		t.Fatalf("sub view: parent %d crumbs %+v", sub.Parent, sub.Crumbs)
	}
}

func TestHideThenRescanBringsBack(t *testing.T) {
	a, ref, _ := setup(t)
	v, _ := a.View(ref, 8, 0)
	small := find(t, v, "small")
	before := v.Root.Size
	if err := a.Hide(small.Ref); err != nil {
		t.Fatal(err)
	}
	v, _ = a.View(ref, 8, 0)
	if find(t, v, "small") != nil || v.Root.Size >= before {
		t.Fatalf("still there, size %d", v.Root.Size)
	}
	if err := a.Rescan(ref); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a, ref)
	v, _ = a.View(ref, 8, 0)
	if find(t, v, "small") == nil || v.Root.Size != before {
		t.Fatalf("not back, size %d, want %d", v.Root.Size, before)
	}
}

func TestUnhideBringsAFolderBack(t *testing.T) {
	a, ref, _ := setup(t)
	v, _ := a.View(ref, 8, 0)
	before := v.Root.Size
	small := find(t, v, "small")
	big := find(t, v, "big")
	for _, r := range []int64{small.Ref, big.Ref} {
		if err := a.Hide(r); err != nil {
			t.Fatal(err)
		}
	}
	hidden := a.Hidden(ref)
	if len(hidden) != 2 || hidden[0].Name != "small" || hidden[0].Size == 0 {
		t.Fatalf("hidden = %+v", hidden)
	}
	if err := a.Unhide(small.Ref); err != nil {
		t.Fatal(err)
	}
	v, _ = a.View(ref, 8, 0)
	if find(t, v, "small") == nil || len(a.Hidden(ref)) != 1 {
		t.Fatalf("small not back, hidden %+v", a.Hidden(ref))
	}
	if err := a.Unhide(small.Ref); err == nil {
		t.Fatal("unhid a folder that is not hidden")
	}
	if err := a.UnhideAll(ref); err != nil {
		t.Fatal(err)
	}
	v, _ = a.View(ref, 8, 0)
	if v.Root.Size != before || len(a.Hidden(ref)) != 0 {
		t.Fatalf("after unhide all: %d, want %d; hidden %+v", v.Root.Size, before, a.Hidden(ref))
	}
}

func TestRescanForgetsHiddenInside(t *testing.T) {
	a, ref, _ := setup(t)
	v, _ := a.View(ref, 8, 0)
	if err := a.Hide(find(t, v, "small").Ref); err != nil {
		t.Fatal(err)
	}
	if err := a.Rescan(ref); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a, ref)
	if h := a.Hidden(ref); len(h) != 0 {
		t.Fatalf("still listed as hidden after rescan: %+v", h)
	}
}

func TestRootCannotBeTrashedOrHidden(t *testing.T) {
	a, ref, root := setup(t)
	if err := a.Trash(ref); err == nil {
		t.Fatal("trashed the root")
	}
	if err := a.Hide(ref); err == nil {
		t.Fatal("hid the root")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
}

func TestActionsRefusedWhileScanning(t *testing.T) {
	a, ref, root := setup(t)
	v, _ := a.View(ref, 8, 0)
	s, _, _ := a.lookup(ref)
	a.mu.Lock()
	s.running = true
	a.mu.Unlock()
	big := find(t, v, "big")
	if err := a.Trash(big.Ref); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "big")); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownRef(t *testing.T) {
	a, _, _ := setup(t)
	if _, err := a.View(encode(99, 0), 8, 0); err == nil {
		t.Fatal("no error")
	}
	if err := a.Trash(-1); err == nil {
		t.Fatal("no error for other segment ref")
	}
}

func TestSecondScanReplacesFirst(t *testing.T) {
	a, ref, root := setup(t)
	ref2, err := a.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a, ref2)
	if _, err := a.View(ref, 8, 0); err == nil {
		t.Fatal("old ref still works")
	}
}

func TestAddVolumeArcsOrder(t *testing.T) {
	v := &tree.ViewNode{Kids: []*tree.ViewNode{{Name: "x", Size: 1}}}
	addVolumeArcs(v, 5, 9)
	if len(v.Kids) != 3 || v.Kids[1].Kind != KindUnscanned || v.Kids[2].Kind != KindFree {
		t.Fatalf("kids: %+v", v.Kids)
	}
	v = &tree.ViewNode{}
	addVolumeArcs(v, -5, 0)
	if len(v.Kids) != 0 {
		t.Fatal("negative unscanned added")
	}
}

func kinds(v *View, kind string) []*tree.ViewNode {
	var out []*tree.ViewNode
	for _, k := range v.Root.Kids {
		if k.Kind == kind {
			out = append(out, k)
		}
	}
	return out
}

func TestFilesShowAtTheCenterAndFurtherOut(t *testing.T) {
	a, ref, root := setup(t)
	if err := os.WriteFile(filepath.Join(root, "top.bin"), make([]byte, 300_000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.Rescan(ref); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a, ref)

	v, err := a.View(ref, 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	files := kinds(v, KindFile)
	real, _ := filepath.EvalSymlinks(root)
	if len(files) != 1 || files[0].Name != "top.bin" || files[0].Size < 300_000 || files[0].Path != filepath.Join(real, "top.bin") {
		t.Fatalf("files at the center: %+v", files)
	}
	var sum int64
	for _, k := range v.Root.Kids {
		sum += k.Size
	}
	if sum != v.Root.Size {
		t.Fatalf("center ring adds up to %d, root is %d", sum, v.Root.Size)
	}
	// big holds the 200 kB file "a": a big share of this small view.
	big := find(t, v, "big")
	var deep []string
	for _, k := range big.Kids {
		if k.Kind == KindFile {
			deep = append(deep, k.Name)
		}
		if k.Kind == tree.KindOther {
			t.Fatal("loose files further out went into other")
		}
	}
	if len(deep) != 1 || deep[0] != "a" {
		t.Fatalf("files in big: %v", deep)
	}
}

func TestOnlyFoldersWithVisibleFilesAreListed(t *testing.T) {
	a, ref, root := setup(t)
	var listed []string
	a.readDir = func(p string) ([]platform.Entry, error) {
		listed = append(listed, p)
		return platform.ReadDir(p)
	}
	// Each folder holds about a third of the bytes. At 50 %, none of their
	// files could be drawn, so only the center is listed.
	if _, err := a.View(ref, 8, 0.5); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(root)
	if len(listed) != 1 || listed[0] != real {
		t.Fatalf("listed %v", listed)
	}
	// Folders in the outermost ring are never listed: their files have no ring.
	s, _, _ := a.lookup(ref)
	a.treeChanged(s) // empty the cache so every listing shows up
	listed = nil
	if _, err := a.View(ref, 1, 0); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("depth 1 listed %v", listed)
	}
}

func TestSmallFilesGoToOther(t *testing.T) {
	a, ref, _ := setup(t)
	v, _ := a.View(ref, 8, 0.9) // everything is below 90 % of the view
	if len(kinds(v, KindFile)) != 0 {
		t.Fatal("small file shown on its own")
	}
	if o := kinds(v, tree.KindOther); len(o) != 1 || o[0].Size != v.Root.Size {
		t.Fatalf("other = %+v, root %d", o, v.Root.Size)
	}
}

func TestHardLinkedFileShowsOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a"), make([]byte, 200_000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "a"), filepath.Join(root, "sub", "a")); err != nil {
		t.Skip("hard links not supported:", err)
	}
	a := New(func(string, any) {})
	ref, err := a.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a, ref)
	// The root is read first, so the file counts there and only there.
	v, _ := a.View(ref, 8, 0)
	if len(kinds(v, KindFile)) != 1 {
		t.Fatalf("root files: %+v", kinds(v, KindFile))
	}
	sub, _ := a.View(encode(decodeIdx(ref), tree.Root+1), 8, 0)
	if len(kinds(sub, KindFile)) != 0 {
		t.Fatalf("sub files: %+v", kinds(sub, KindFile))
	}
}

func decodeIdx(ref int64) int64 { idx, _ := decode(ref); return idx }

func TestTrashFile(t *testing.T) {
	a, ref, root := setup(t)
	v, _ := a.View(ref, 8, 0)
	big := find(t, v, "big")
	before := v.Root.Size
	for _, bad := range []string{"", ".", "..", "sub", "../small/c", "missing"} {
		if err := a.TrashFile(big.Ref, bad); err == nil {
			t.Fatalf("trashed %q", bad)
		}
	}
	// Never touch the real trash from a test.
	var trashed []string
	a.trash = func(p string) error { trashed = append(trashed, p); return os.Remove(p) }
	if err := a.TrashFile(big.Ref, "a"); err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 1 {
		t.Fatalf("trashed %v", trashed)
	}
	if _, err := os.Stat(filepath.Join(root, "big", "a")); !os.IsNotExist(err) {
		t.Fatal("file still there")
	}
	v, _ = a.View(ref, 8, 0)
	if v.Root.Size >= before || v.Root.Size < before-300_000 {
		t.Fatalf("size %d, was %d", v.Root.Size, before)
	}
	sub, _ := a.View(big.Ref, 8, 0)
	for _, f := range kinds(sub, KindFile) {
		if f.Name == "a" {
			t.Fatal("trashed file still listed")
		}
	}
}

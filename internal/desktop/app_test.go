package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

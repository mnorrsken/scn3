package tree

import (
	"path/filepath"
	"testing"
	"unsafe"
)

// sample builds: root(10b/1f) -> a(100b/2f) -> c(1000b/3f); root -> b(1b/1f)
func sample(t *testing.T) (tr *Tree, a, b, c ID) {
	t.Helper()
	tr = New(filepath.FromSlash("/r"))
	a = tr.Add(Root, 10, 1, 0, []Child{{Name: "a"}, {Name: "b"}}, false)
	b = a + 1
	c = tr.Add(a, 100, 2, 0, []Child{{Name: "c"}}, false)
	tr.Add(b, 1, 1, 0, nil, false)
	tr.Add(c, 1000, 3, 0, nil, false)
	return tr, a, b, c
}

func TestNodeStaysCompact(t *testing.T) {
	if s := unsafe.Sizeof(node{}); s > 56 {
		t.Fatalf("node is %d bytes, want at most 56", s)
	}
}

func TestTotalsPropagate(t *testing.T) {
	tr, a, _, c := sample(t)
	if s, f := tr.Size(Root); s != 1111 || f != 7 {
		t.Fatalf("root = %d bytes %d files", s, f)
	}
	if s, f := tr.Size(a); s != 1100 || f != 5 {
		t.Fatalf("a = %d bytes %d files", s, f)
	}
	if got, want := tr.Path(c), filepath.FromSlash("/r/a/c"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestRemoveAndClear(t *testing.T) {
	tr, a, _, c := sample(t)
	tr.Remove(c)
	if s, f := tr.Size(Root); s != 111 || f != 4 {
		t.Fatalf("after remove: %d bytes %d files", s, f)
	}
	if tr.Valid(c) {
		t.Fatal("removed node still valid")
	}
	tr.Remove(c) // second remove must not subtract again
	if s, _ := tr.Size(Root); s != 111 {
		t.Fatalf("double remove: %d", s)
	}
	tr.Clear(a)
	if s, f := tr.Size(Root); s != 11 || f != 2 {
		t.Fatalf("after clear: %d bytes %d files", s, f)
	}
	tr.Add(a, 5, 1, 0, nil, false)
	if s, f := tr.Size(Root); s != 16 || f != 3 {
		t.Fatalf("after re-add: %d bytes %d files", s, f)
	}
}

func TestDeniedCount(t *testing.T) {
	tr := New("/r")
	a := tr.Add(Root, 0, 0, 0, []Child{{Name: "a"}, {Name: "b"}}, false)
	tr.Add(a, 0, 0, 0, nil, true)
	tr.Add(a+1, 0, 0, 0, nil, true)
	if d := tr.Denied(); d != 2 {
		t.Fatalf("denied = %d", d)
	}
	tr.Remove(a)
	if d := tr.Denied(); d != 1 {
		t.Fatalf("denied after remove = %d", d)
	}
	tr.Clear(Root)
	if d := tr.Denied(); d != 0 {
		t.Fatalf("denied after clear = %d", d)
	}
}

func sum(v *ViewNode) int64 {
	var s int64
	for _, k := range v.Kids {
		s += k.Size
	}
	return s
}

func TestViewCullsAndKeepsSum(t *testing.T) {
	tr, _, _, _ := sample(t)
	v := tr.View(Root, 8, 5, true)
	if sum(v) != v.Size {
		t.Fatalf("root kids sum %d, size %d", sum(v), v.Size)
	}
	// a (1100), files here (10), other (b, 1 byte).
	if len(v.Kids) != 3 || v.Kids[0].Name != "a" || v.Kids[1].Kind != KindFiles || v.Kids[2].Kind != KindOther {
		t.Fatalf("unexpected kids: %+v %+v", v.Kids[0], v.Kids[1])
	}
	if v.Kids[2].Size != 1 || v.Kids[2].Ref != -1 {
		t.Fatalf("other = %+v", v.Kids[2])
	}
	if sum(v.Kids[0]) != v.Kids[0].Size {
		t.Fatalf("a kids sum %d, size %d", sum(v.Kids[0]), v.Kids[0].Size)
	}
}

func TestViewDepthAndSort(t *testing.T) {
	tr, _, _, _ := sample(t)
	v := tr.View(Root, 1, 0, true)
	for i, k := range v.Kids {
		if k.Kids != nil {
			t.Fatalf("kid %q has kids beyond the depth limit", k.Name)
		}
		if i > 0 && v.Kids[i-1].Size < k.Size {
			t.Fatal("kids not sorted by size")
		}
	}
}

func TestViewSkipsRemoved(t *testing.T) {
	tr, a, _, _ := sample(t)
	tr.Remove(a)
	for _, k := range tr.View(Root, 8, 0, true).Kids {
		if k.Name == "a" {
			t.Fatal("removed node in view")
		}
	}
}

func TestViewWithoutFilesLeavesAGap(t *testing.T) {
	tr, _, _, _ := sample(t)
	v := tr.View(Root, 8, 5, false)
	for _, k := range v.Kids {
		if k.Kind == KindFiles {
			t.Fatal("files segment in view")
		}
	}
	// a (1100) + other (b: 1); the root's own 10 bytes are left out.
	if got := sum(v); got != 1101 {
		t.Fatalf("kids sum %d", got)
	}
}

func TestRemoveFile(t *testing.T) {
	tr, a, _, _ := sample(t)
	tr.RemoveFile(a, 60)
	if s, f := tr.Size(Root); s != 1051 || f != 6 {
		t.Fatalf("root = %d bytes %d files", s, f)
	}
	if own, f := tr.Own(a); own != 40 || f != 1 {
		t.Fatalf("a own = %d bytes %d files", own, f)
	}
}

func TestTimesPropagateAndShrinkOnRemove(t *testing.T) {
	tr := New("/r")
	a := tr.Add(Root, 0, 1, 100, []Child{{Name: "a", Mtime: 50}, {Name: "b", Mtime: 60}}, false)
	b := a + 1
	tr.Add(a, 10, 1, 300, nil, false) // views leave out empty folders, so give them bytes
	tr.Add(b, 10, 1, 200, nil, false)
	times := func() (root, ka, kb int64) {
		v := tr.View(Root, 2, 0, true)
		for _, k := range v.Kids {
			switch k.Name {
			case "a":
				ka = k.Mtime
			case "b":
				kb = k.Mtime
			}
		}
		return v.Mtime, ka, kb
	}
	if r, ka, kb := times(); r != 300 || ka != 300 || kb != 200 {
		t.Fatalf("root %d, a %d, b %d", r, ka, kb)
	}
	tr.Remove(a)
	if r, _, _ := times(); r != 200 {
		t.Fatalf("after removing a, root is %d", r)
	}
	tr.Clear(b)
	// b is empty now; the newest thing left is the root's own file (100).
	if r, _, _ := times(); r != 100 {
		t.Fatalf("after clearing b, root is %d", r)
	}
}

func TestCrumbs(t *testing.T) {
	tr, _, _, c := sample(t)
	cr := tr.Crumbs(c)
	if len(cr) != 3 || cr[0].Ref != 0 || cr[1].Name != "a" || cr[2].Name != "c" {
		t.Fatalf("crumbs = %+v", cr)
	}
}

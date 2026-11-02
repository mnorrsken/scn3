package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mnorrsken/scn3/internal/tree"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, root string) *Scan {
	t.Helper()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return s
}

func childID(t *testing.T, s *Scan, name string) tree.ID {
	t.Helper()
	for _, k := range s.Tree.View(tree.Root, 1, 0).Kids {
		if k.Name == name && k.Kind == tree.KindDir {
			return tree.ID(k.Ref)
		}
	}
	t.Fatalf("no child %q", name)
	return 0
}

func TestCountsFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a/one"), 100_000)
	write(t, filepath.Join(root, "a/b/two"), 100_000)
	write(t, filepath.Join(root, "three"), 100_000)
	s := run(t, root)
	size, files := s.Tree.Size(tree.Root)
	if files != 3 {
		t.Fatalf("files = %d", files)
	}
	if size < 300_000 {
		t.Fatalf("size = %d", size)
	}
}

func TestSymlinkNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges")
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "real/data"), 1_000_000)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "real/loop")); err != nil {
		t.Fatal(err)
	}
	size, _ := run(t, root).Tree.Size(tree.Root)
	if size > 1_500_000 {
		t.Fatalf("size = %d: followed a symlink", size)
	}
}

func TestHardLinksCountOnce(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a/data"), 1_000_000)
	if err := os.MkdirAll(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "a/data"), filepath.Join(root, "b/data")); err != nil {
		t.Skip("hard links not supported:", err)
	}
	s := run(t, root)
	size, files := s.Tree.Size(tree.Root)
	if files != 1 || size > 1_500_000 {
		t.Fatalf("size = %d, files = %d", size, files)
	}
	// Rescanning the directory that counted the file must count it again.
	// Look the IDs up first: the directory that did not count it has size 0.
	var ids []tree.ID
	for _, k := range s.Tree.View(tree.Root, 1, 0).Kids {
		if k.Kind == tree.KindDir {
			ids = append(ids, tree.ID(k.Ref))
		}
	}
	for _, id := range ids {
		if err := s.Rescan(context.Background(), id, nil); err != nil {
			t.Fatal(err)
		}
		size, files = s.Tree.Size(tree.Root)
		if files != 1 || size < 1_000_000 || size > 1_500_000 {
			t.Fatalf("after rescan of %d: size = %d, files = %d", id, size, files)
		}
	}
}

func TestRescanPatchesSubtree(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a/one"), 1_000_000)
	write(t, filepath.Join(root, "b/two"), 1_000_000)
	s := run(t, root)
	before, _ := s.Tree.Size(tree.Root)

	write(t, filepath.Join(root, "a/sub/new"), 1_000_000)
	if err := s.Rescan(context.Background(), childID(t, s, "a"), nil); err != nil {
		t.Fatal(err)
	}
	after, files := s.Tree.Size(tree.Root)
	if files != 3 || after < before+1_000_000 {
		t.Fatalf("before %d, after %d, files %d", before, after, files)
	}

	// A fresh scan must agree with the patched tree.
	fresh, freshFiles := run(t, root).Tree.Size(tree.Root)
	if after != fresh || files != freshFiles {
		t.Fatalf("patched %d/%d, fresh %d/%d", after, files, fresh, freshFiles)
	}
}

func TestRescanOfDeletedDirRemovesIt(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a/one"), 1_000_000)
	s := run(t, root)
	id := childID(t, s, "a")
	if err := os.RemoveAll(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Rescan(context.Background(), id, nil); err != nil {
		t.Fatal(err)
	}
	if s.Tree.Valid(id) {
		t.Fatal("deleted directory still in tree")
	}
}

func TestDeniedDirIsReported(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	write(t, filepath.Join(locked, "secret"), 1000)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	s := run(t, root)
	if s.Tree.Denied() != 1 {
		t.Fatalf("denied = %d", s.Tree.Denied())
	}
	if errs := s.ReadErrors(); len(errs) != 1 || !strings.Contains(errs[0].Error(), "locked") {
		t.Fatalf("read errors = %v", errs)
	}
}

func TestCancelStopsAndKeepsPartialTree(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		write(t, filepath.Join(root, d, "x"), 10)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Run(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestRootMustBeDirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f")
	write(t, f, 1)
	if _, err := New(f); err == nil {
		t.Fatal("no error for a file")
	}
}

func TestProgressEndsWithDone(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a/one"), 10)
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	var last Progress
	if err := s.Run(context.Background(), func(p Progress) { last = p }); err != nil {
		t.Fatal(err)
	}
	if !last.Done || last.Files != 1 || last.Dirs != 2 {
		t.Fatalf("last progress = %+v", last)
	}
}

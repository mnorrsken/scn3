package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/mnorrsken/scn3/internal/tree"
)

func TestAPFSClonesCountOnce(t *testing.T) {
	root := t.TempDir()
	const size = 1_000_000
	orig := filepath.Join(root, "a/orig")
	write(t, orig, size)
	for _, d := range []string{"b", "c"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Clonefile(orig, filepath.Join(root, "b/clone"), 0); err != nil {
		t.Skip("clones not supported here:", err)
	}
	if err := unix.Clonefile(orig, filepath.Join(root, "c/clone"), 0); err != nil {
		t.Fatal(err)
	}

	s := run(t, root)
	got, files := s.Tree.Size(tree.Root)
	if files != 1 || got < size || got > size*3/2 {
		t.Fatalf("three clones: %d bytes, %d files", got, files)
	}

	// A plain copy of the same bytes really takes its own space.
	write(t, filepath.Join(root, "plain"), size)
	got, files = run(t, root).Tree.Size(tree.Root)
	if files != 2 || got < 2*size || got > size*5/2 {
		t.Fatalf("clones plus a copy: %d bytes, %d files", got, files)
	}
}

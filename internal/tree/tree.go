// Package tree holds the scanned directory tree in a compact arena.
//
// Only directories are nodes. Each directory keeps a total for its loose
// files, names live in one shared byte arena, and the children of a directory
// are a contiguous index range. That keeps a node at 48 bytes.
//
// All methods are safe for concurrent use.
package tree

import (
	"path/filepath"
	"sort"
	"sync"
)

// ID identifies a directory in a Tree. The root is always 0.
type ID = uint32

const (
	// Root is the ID of the root directory.
	Root ID = 0

	noParent = ^uint32(0)
)

const (
	flagDenied uint16 = 1 << iota
	flagRemoved
)

type node struct {
	parent  ID
	first   ID
	nkids   uint32
	nameOff uint32
	nameLen uint16
	flags   uint16
	own     int64  // bytes of loose files plus the directory entry itself
	size    int64  // own plus all live descendants
	ownFile uint32 // loose files directly in this directory
	files   uint32 // ownFile plus all live descendants
}

// Child describes a subdirectory passed to AddDir.
type Child struct {
	Name string
	Own  int64 // on-disk size of the directory entry itself
}

// Tree is a directory tree rooted at a filesystem path.
type Tree struct {
	mu     sync.RWMutex
	path   string
	nodes  []node
	names  []byte
	denied int
}

// New returns a tree with only a root directory.
func New(rootPath string) *Tree {
	return &Tree{path: rootPath, nodes: []node{{parent: noParent}}}
}

// Add records what a scan found directly in directory id: loose file bytes,
// the loose file count and subdirectories. It returns the ID of the first
// subdirectory; the rest follow in order.
func (t *Tree) Add(id ID, bytes int64, files uint32, kids []Child, denied bool) ID {
	t.mu.Lock()
	defer t.mu.Unlock()

	first := ID(len(t.nodes))
	var kidBytes int64
	for _, k := range kids {
		off := uint32(len(t.names))
		t.names = append(t.names, k.Name...)
		t.nodes = append(t.nodes, node{
			parent:  id,
			nameOff: off,
			nameLen: uint16(len(k.Name)),
			own:     k.Own,
			size:    k.Own,
		})
		kidBytes += k.Own
	}

	n := &t.nodes[id]
	n.first = first
	n.nkids = uint32(len(kids))
	n.own += bytes
	n.ownFile += files
	if denied {
		n.flags |= flagDenied
		t.denied++
	}
	t.propagate(id, bytes+kidBytes, int64(files))
	return first
}

// propagate adds deltas to id and all its ancestors.
func (t *Tree) propagate(id ID, bytes, files int64) {
	for i := id; i != noParent; i = t.nodes[i].parent {
		t.nodes[i].size += bytes
		t.nodes[i].files = uint32(int64(t.nodes[i].files) + files)
	}
}

// SetOwn sets the on-disk size of directory id's own entry. Use it for the
// root, which has no parent to report it, or after Clear.
func (t *Tree) SetOwn(id ID, own int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delta := own - t.nodes[id].own
	t.nodes[id].own = own
	t.propagate(id, delta, 0)
}

// Clear empties directory id so that it can be scanned again. Ancestor totals
// are reduced to match. The old descendants stay in the arena as garbage.
func (t *Tree) Clear(id ID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := &t.nodes[id]
	size, files := n.size, n.files
	t.denied -= t.countDenied(id)
	n.first, n.nkids, n.own, n.size, n.ownFile, n.files = 0, 0, 0, 0, 0, 0
	n.flags &^= flagDenied
	if n.parent != noParent {
		t.propagate(n.parent, -size, -int64(files))
	}
}

// Remove drops directory id and its subtree from the totals and from views.
func (t *Tree) Remove(id ID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := &t.nodes[id]
	if id == Root || n.flags&flagRemoved != 0 {
		return
	}
	t.denied -= t.countDenied(id)
	n.flags |= flagRemoved
	t.propagate(n.parent, -n.size, -int64(n.files))
}

func (t *Tree) countDenied(id ID) int {
	n := &t.nodes[id]
	if n.flags&flagRemoved != 0 {
		return 0
	}
	c := 0
	if n.flags&flagDenied != 0 {
		c = 1
	}
	for i := n.first; i < n.first+n.nkids; i++ {
		c += t.countDenied(i)
	}
	return c
}

// Size returns the total bytes and file count under id.
func (t *Tree) Size(id ID) (bytes int64, files int64) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.nodes[id].size, int64(t.nodes[id].files)
}

// Denied returns how many live directories could not be read fully.
func (t *Tree) Denied() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.denied
}

// Valid reports whether id is a live directory of this tree.
func (t *Tree) Valid(id ID) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if int(id) >= len(t.nodes) {
		return false
	}
	for i := id; i != noParent; i = t.nodes[i].parent {
		if t.nodes[i].flags&flagRemoved != 0 {
			return false
		}
	}
	return true
}

// IsUnder reports whether id is ancestor itself or a descendant of it.
func (t *Tree) IsUnder(id, ancestor ID) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for i := id; i != noParent; i = t.nodes[i].parent {
		if i == ancestor {
			return true
		}
	}
	return false
}

func (t *Tree) name(n *node) string {
	return string(t.names[n.nameOff : n.nameOff+uint32(n.nameLen)])
}

// Path returns the filesystem path of id.
func (t *Tree) Path(id ID) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.pathLocked(id)
}

func (t *Tree) pathLocked(id ID) string {
	var parts []string
	for i := id; i != Root; i = t.nodes[i].parent {
		parts = append(parts, t.name(&t.nodes[i]))
	}
	elems := make([]string, 0, len(parts)+1)
	elems = append(elems, t.path)
	for i := len(parts) - 1; i >= 0; i-- {
		elems = append(elems, parts[i])
	}
	return filepath.Join(elems...)
}

// Parent returns the parent of id. ok is false for the root.
func (t *Tree) Parent(id ID) (parent ID, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p := t.nodes[id].parent
	return p, p != noParent
}

// Kinds of view nodes.
const (
	KindDir   = "dir"
	KindFiles = "files" // loose files of a directory
	KindOther = "other" // siblings too small to draw, merged
)

// ViewNode is one segment of a view.
type ViewNode struct {
	Ref    int64       `json:"ref"` // directory ID; the parent's for files, -1 for other
	Name   string      `json:"name"`
	Size   int64       `json:"size"`
	Files  int64       `json:"files"`
	Kind   string      `json:"kind"`
	Denied bool        `json:"denied,omitempty"`
	Path   string      `json:"path,omitempty"` // set only where it cannot be derived from the parent
	Kids   []*ViewNode `json:"kids,omitempty"`
}

// View returns the subtree under id, at most maxDepth rings deep. Segments
// smaller than minBytes are merged per parent into one "other" segment so that
// the sizes still add up.
func (t *Tree) View(id ID, maxDepth int, minBytes int64) *ViewNode {
	t.mu.RLock()
	defer t.mu.RUnlock()
	root := t.viewNode(id, maxDepth, minBytes)
	root.Path = t.pathLocked(id)
	return root
}

func (t *Tree) viewNode(id ID, depth int, minBytes int64) *ViewNode {
	n := &t.nodes[id]
	v := &ViewNode{
		Ref:    int64(id),
		Name:   t.name(n),
		Size:   n.size,
		Files:  int64(n.files),
		Kind:   KindDir,
		Denied: n.flags&flagDenied != 0,
	}
	if depth <= 0 {
		return v
	}

	big := make([]ID, 0, n.nkids)
	var otherSize, otherFiles int64
	for i := n.first; i < n.first+n.nkids; i++ {
		c := &t.nodes[i]
		if c.flags&flagRemoved != 0 || c.size == 0 {
			continue
		}
		if c.size < minBytes {
			otherSize += c.size
			otherFiles += int64(c.files)
			continue
		}
		big = append(big, i)
	}

	kids := make([]*ViewNode, 0, len(big)+2)
	for _, i := range big {
		kids = append(kids, t.viewNode(i, depth-1, minBytes))
	}
	if n.own > 0 {
		if n.own < minBytes {
			otherSize += n.own
			otherFiles += int64(n.ownFile)
		} else {
			kids = append(kids, &ViewNode{
				Ref:   int64(id),
				Name:  "files here",
				Size:  n.own,
				Files: int64(n.ownFile),
				Kind:  KindFiles,
			})
		}
	}
	sort.SliceStable(kids, func(a, b int) bool { return kids[a].Size > kids[b].Size })
	if otherSize > 0 {
		kids = append(kids, &ViewNode{Ref: -1, Name: "other", Size: otherSize, Files: otherFiles, Kind: KindOther})
	}
	v.Kids = kids
	return v
}

// Crumb is one step of the path from the root to a directory.
type Crumb struct {
	Ref  int64  `json:"ref"`
	Name string `json:"name"`
}

// Crumbs returns the directories from the root down to id, inclusive.
func (t *Tree) Crumbs(id ID) []Crumb {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []Crumb
	for i := id; i != noParent; i = t.nodes[i].parent {
		out = append(out, Crumb{Ref: int64(i), Name: t.name(&t.nodes[i])})
	}
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return out
}

// Package desktop is the API the UI calls. It owns the running scans and
// turns tree views into what the chart needs. Only main.go wires it to Wails.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mnorrsken/scn3/internal/platform"
	"github.com/mnorrsken/scn3/internal/scanner"
	"github.com/mnorrsken/scn3/internal/tree"
)

// Extra segment kinds next to the ones in package tree.
const (
	KindFile      = "file"      // one file, shown only in the ring around the center
	KindFree      = "free"      // free space on the volume
	KindUnscanned = "unscanned" // used on the volume but not found by the scan
)

const refShift = 32

// ErrBusy is returned for actions on a tree that is still being scanned.
var ErrBusy = errors.New("wait for the scan to finish or cancel it")

// ProgressEvent is the event name for scan progress.
const ProgressEvent = "scan:progress"

// ScanProgress is the payload of ProgressEvent.
type ScanProgress struct {
	scanner.Progress
	Scan int64 `json:"scan"` // ref of the scan's root
}

// View is what the UI draws.
type View struct {
	Root        *tree.ViewNode `json:"root"`
	Crumbs      []tree.Crumb   `json:"crumbs"`
	Parent      int64          `json:"parent"` // ref to go up to, -1 at the top
	Sep         string         `json:"sep"`
	VolumeBytes int64          `json:"volumeBytes"` // base for "% of volume"
	Scanning    bool           `json:"scanning"`
	Denied      int            `json:"denied"`
}

type session struct {
	scan    *scanner.Scan
	idx     int64
	volume  bool
	name    string
	cancel  context.CancelFunc
	running bool
	files   map[tree.ID][]platform.Entry // file listings of directories shown at the center
}

// App is bound to the webview. Methods are called from the UI.
type App struct {
	emit    func(event string, data any)
	trash   func(path string) error                     // platform.Trash; tests swap it out
	readDir func(path string) ([]platform.Entry, error) // platform.ReadDir; tests count calls

	mu     sync.Mutex
	nextID int64
	byIdx  map[int64]*session
	byPath map[string]*session
}

// New returns an App that sends events through emit.
func New(emit func(event string, data any)) *App {
	return &App{emit: emit, trash: platform.Trash, readDir: platform.ReadDir, byIdx: map[int64]*session{}, byPath: map[string]*session{}}
}

// SetEmit replaces the event sink; Wails only provides one after startup.
func (a *App) SetEmit(emit func(event string, data any)) { a.emit = emit }

// Shutdown cancels all running scans.
func (a *App) Shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.byIdx {
		if s.cancel != nil {
			s.cancel()
		}
	}
}

// Volumes lists the mounted volumes.
func (a *App) Volumes() ([]platform.Volume, error) {
	return platform.Volumes()
}

func encode(idx int64, id tree.ID) int64 { return idx<<refShift | int64(id) }

func decode(ref int64) (int64, tree.ID) { return ref >> refShift, tree.ID(ref & (1<<refShift - 1)) }

func (a *App) lookup(ref int64) (*session, tree.ID, error) {
	idx, id := decode(ref)
	a.mu.Lock()
	s := a.byIdx[idx]
	a.mu.Unlock()
	if ref < 0 || s == nil || !s.scan.Tree.Valid(id) {
		return nil, 0, fmt.Errorf("that folder is no longer in the scan")
	}
	return s, id, nil
}

// Scan starts scanning path and returns the ref of its root. A previous scan
// of the same path is replaced.
func (a *App) Scan(path string) (int64, error) {
	sc, err := scanner.New(path)
	if err != nil {
		return 0, err
	}
	volume := false
	name := filepath.Base(sc.Root)
	if vols, err := platform.Volumes(); err == nil {
		for _, v := range vols {
			if filepath.Clean(v.Path) == sc.Root {
				volume, name = true, v.Name
			}
		}
	}

	a.mu.Lock()
	if old := a.byPath[sc.Root]; old != nil {
		if old.cancel != nil {
			old.cancel()
		}
		delete(a.byIdx, old.idx)
	}
	a.nextID++
	s := &session{scan: sc, idx: a.nextID, volume: volume, name: name}
	a.byIdx[s.idx] = s
	a.byPath[sc.Root] = s
	a.mu.Unlock()

	a.start(s, tree.Root, func(ctx context.Context, cb func(scanner.Progress)) error {
		return sc.Run(ctx, cb)
	})
	return encode(s.idx, tree.Root), nil
}

// start runs a walk in the background and reports progress.
func (a *App) start(s *session, id tree.ID, walk func(context.Context, func(scanner.Progress)) error) {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	s.cancel, s.running = cancel, true
	a.mu.Unlock()
	go func() {
		defer cancel()
		root := encode(s.idx, id)
		send := func(p scanner.Progress) {
			if p.Done {
				a.mu.Lock()
				s.running = false
				a.mu.Unlock()
			}
			a.emit(ProgressEvent, ScanProgress{Progress: p, Scan: root})
		}
		// Cancelling leaves a usable partial tree, so the error carries no news.
		_ = walk(ctx, send)
	}()
}

// filesOf lists the files directly in directory id, cached until the tree changes.
func (a *App) filesOf(s *session, id tree.ID) []platform.Entry {
	a.mu.Lock()
	cached, ok := s.files[id]
	a.mu.Unlock()
	if ok {
		return cached
	}
	// A partial listing is still worth showing; what is missing stays in "other".
	entries, _ := a.readDir(s.scan.Tree.Path(id))
	files := entries[:0]
	for _, e := range entries {
		if !e.IsDir {
			files = append(files, e)
		}
	}
	a.mu.Lock()
	if s.files == nil {
		s.files = map[tree.ID][]platform.Entry{}
	}
	if len(s.files) >= 4*maxListedDirs {
		s.files = map[tree.ID][]platform.Entry{} // keep the cache from growing without end
	}
	s.files[id] = files
	a.mu.Unlock()
	return files
}

// treeChanged drops cached file listings.
func (a *App) treeChanged(s *session) {
	a.mu.Lock()
	s.files = nil
	a.mu.Unlock()
}

// maxListedDirs caps how many directories one view lists files for.
const maxListedDirs = 200

// addFiles gives files their own segments, around the center and in any
// directory further out whose loose files take up enough of the chart for at
// least one file to be drawn. A directory with less than that is never listed,
// so the cost follows what is visible, not how big the tree is. The rest stays
// an empty part of the directory's arc; around the center it goes into "other".
func (a *App) addFiles(v *tree.ViewNode, maxDepth int, minBytes int64) {
	minBytes = max(minBytes, 1)
	type job struct {
		s      *session
		id     tree.ID
		node   *tree.ViewNode
		own    int64
		center bool
		files  []platform.Entry
	}
	var jobs []*job
	// kidsRing is the ring that n's kids are drawn in; 0 is around the center.
	var walk func(n *tree.ViewNode, kidsRing int)
	walk = func(n *tree.ViewNode, kidsRing int) {
		if kidsRing >= maxDepth {
			return
		}
		if n.Kind == tree.KindDir && n.Ref >= 0 {
			idx, id := decode(n.Ref)
			a.mu.Lock()
			s := a.byIdx[idx]
			// Listing files waits for the scan: it needs the final hard-link table.
			idle := s != nil && !s.running
			a.mu.Unlock()
			if idle {
				own, _ := s.scan.Tree.Own(id)
				if center := kidsRing == 0; center || own >= minBytes {
					jobs = append(jobs, &job{s: s, id: id, node: n, own: own, center: center})
				}
			}
		}
		for _, k := range n.Kids {
			if k.Kind == tree.KindDir {
				walk(k, kidsRing+1)
			}
		}
	}
	walk(v, 0)
	sort.SliceStable(jobs, func(i, j int) bool {
		if jobs[i].center != jobs[j].center {
			return jobs[i].center
		}
		return jobs[i].own > jobs[j].own
	})
	if len(jobs) > maxListedDirs {
		jobs = jobs[:maxListedDirs]
	}

	// Directory reads are mostly waiting on the disk, so do several at once.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			j.files = a.filesOf(j.s, j.id)
		}()
	}
	wg.Wait()
	for _, j := range jobs {
		placeFiles(j.s, j.id, j.node, j.files, minBytes, j.center)
	}
}

// placeFiles adds the files of directory id that are at least minBytes as
// segments of v. Files counted in another directory (hard links, clones) are
// left out. Around the center, the rest of the loose files and the size of the
// directory entry itself go into "other".
func placeFiles(s *session, id tree.ID, v *tree.ViewNode, files []platform.Entry, minBytes int64, center bool) {
	own, ownFiles := s.scan.Tree.Own(id)
	dir := s.scan.Tree.Path(id)
	var shown []*tree.ViewNode
	var sum int64
	for _, e := range files {
		if e.Size < minBytes || !s.scan.Counted(id, e) {
			continue
		}
		shown = append(shown, &tree.ViewNode{
			Ref:   v.Ref,
			Name:  e.Name,
			Size:  e.Size,
			Files: 1,
			Kind:  KindFile,
			Path:  filepath.Join(dir, e.Name),
		})
		sum += e.Size
	}
	sort.Slice(shown, func(i, j int) bool { return shown[i].Size > shown[j].Size })
	// Files that grew since the scan could add up to more than was counted.
	for sum > own && len(shown) > 0 {
		sum -= shown[len(shown)-1].Size
		shown = shown[:len(shown)-1]
	}

	var other *tree.ViewNode
	kids := make([]*tree.ViewNode, 0, len(v.Kids)+len(shown))
	for _, k := range v.Kids {
		if k.Kind == tree.KindOther {
			other = k
		} else {
			kids = append(kids, k)
		}
	}
	kids = append(kids, shown...)
	sort.SliceStable(kids, func(i, j int) bool { return kids[i].Size > kids[j].Size })
	if rest := own - sum; center && rest > 0 {
		if other == nil {
			other = &tree.ViewNode{Ref: -1, Name: "other", Kind: tree.KindOther}
		}
		other.Size += rest
		other.Files += max(ownFiles-int64(len(shown)), 0)
	}
	if other != nil {
		kids = append(kids, other)
	}
	v.Kids = kids
}

// fileIn checks that name is a plain file name and finds it in directory id.
func fileIn(s *session, id tree.ID, name string) (platform.Entry, string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return platform.Entry{}, "", fmt.Errorf("%q is not a file name", name)
	}
	dir := s.scan.Tree.Path(id)
	path := filepath.Join(dir, name)
	entries, err := platform.ReadDir(dir)
	for _, e := range entries {
		if e.Name == name && !e.IsDir {
			return e, path, nil
		}
	}
	if err != nil {
		return platform.Entry{}, "", fmt.Errorf("reading %s: %w", dir, err)
	}
	return platform.Entry{}, "", fmt.Errorf("%s no longer exists", path)
}

// TrashFile moves the file name in the directory at ref to the trash.
func (a *App) TrashFile(ref int64, name string) error {
	s, id, err := a.idle(ref)
	if err != nil {
		return err
	}
	e, path, err := fileIn(s, id, name)
	if err != nil {
		return err
	}
	if err := a.trash(path); err != nil {
		return err
	}
	s.scan.RemoveFile(id, e)
	a.treeChanged(s)
	return nil
}

// RevealFile selects the file name in the directory at ref in the file manager.
func (a *App) RevealFile(ref int64, name string) error {
	s, id, err := a.lookup(ref)
	if err != nil {
		return err
	}
	_, path, err := fileIn(s, id, name)
	if err != nil {
		return err
	}
	return platform.Reveal(path)
}

// Cancel stops the scan that contains ref.
func (a *App) Cancel(ref int64) {
	idx, _ := decode(ref)
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.byIdx[idx]; s != nil && s.cancel != nil {
		s.cancel()
	}
}

// Rescan scans the directory at ref again and patches it into the tree.
func (a *App) Rescan(ref int64) error {
	s, id, err := a.idle(ref)
	if err != nil {
		return err
	}
	a.treeChanged(s)
	a.start(s, id, func(ctx context.Context, cb func(scanner.Progress)) error {
		return s.scan.Rescan(ctx, id, cb)
	})
	return nil
}

func (a *App) idle(ref int64) (*session, tree.ID, error) {
	s, id, err := a.lookup(ref)
	if err != nil {
		return nil, 0, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.running {
		return nil, 0, ErrBusy
	}
	return s, id, nil
}

// Trash moves the directory at ref to the trash and drops it from the tree.
func (a *App) Trash(ref int64) error {
	s, id, err := a.idle(ref)
	if err != nil {
		return err
	}
	if id == tree.Root {
		return errors.New("the root of a scan cannot be moved to the trash")
	}
	if err := a.trash(s.scan.Tree.Path(id)); err != nil {
		return err
	}
	s.scan.Remove(id)
	a.treeChanged(s)
	return nil
}

// Hide drops the directory at ref from the chart. Nothing changes on disk; a
// rescan of a parent brings it back.
func (a *App) Hide(ref int64) error {
	s, id, err := a.idle(ref)
	if err != nil {
		return err
	}
	if id == tree.Root {
		return errors.New("the root of a scan cannot be hidden")
	}
	s.scan.Tree.Remove(id)
	a.treeChanged(s)
	return nil
}

// Reveal shows the directory at ref in the file manager.
func (a *App) Reveal(ref int64) error {
	s, id, err := a.lookup(ref)
	if err != nil {
		return err
	}
	path := s.scan.Tree.Path(id)
	if id == tree.Root {
		return platform.RevealDir(path)
	}
	return platform.Reveal(path)
}

// View returns the culled subtree under ref. minFrac is the share of the view
// below which a segment is too small to draw.
func (a *App) View(ref int64, maxDepth int, minFrac float64) (*View, error) {
	s, id, err := a.lookup(ref)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	running := s.running
	a.mu.Unlock()

	t := s.scan.Tree
	size, _ := t.Size(id)
	whole, _ := t.Size(tree.Root)
	var capacity, free int64
	if s.volume {
		// Read fresh each time: trashing or a running scan changes it.
		if c, f, err := platform.Usage(s.scan.Root); err == nil {
			capacity, free = c, f
		}
	}

	span := size
	base := whole
	atVolume := s.volume && id == tree.Root && capacity > 0
	if s.volume && capacity > 0 {
		base = capacity
	}
	if atVolume {
		span = max(capacity, size+free)
	}
	minBytes := int64(minFrac * float64(span))
	v := t.View(id, maxDepth, minBytes, false)
	if id == tree.Root {
		v.Name = s.name
	}
	offset(v, s.idx)
	a.addFiles(v, maxDepth, minBytes)
	if atVolume {
		addVolumeArcs(v, capacity-free-size, free)
	}

	crumbs := t.Crumbs(id)
	for i := range crumbs {
		crumbs[i].Ref = encode(s.idx, tree.ID(crumbs[i].Ref))
	}
	crumbs[0].Name = s.name

	parent := int64(-1)
	if p, ok := t.Parent(id); ok {
		parent = encode(s.idx, p)
	}
	return &View{
		Root:        v,
		Crumbs:      crumbs,
		Parent:      parent,
		Sep:         string(filepath.Separator),
		VolumeBytes: base,
		Scanning:    running,
		Denied:      t.Denied(),
	}, nil
}

// addVolumeArcs appends the not-scanned and free arcs to a volume root's kids.
func addVolumeArcs(v *tree.ViewNode, unscanned, free int64) {
	if unscanned > 0 {
		v.Kids = append(v.Kids, &tree.ViewNode{Ref: -1, Name: "not scanned", Size: unscanned, Kind: KindUnscanned})
	}
	if free > 0 {
		v.Kids = append(v.Kids, &tree.ViewNode{Ref: -1, Name: "free", Size: free, Kind: KindFree})
	}
}

// offset turns tree IDs into refs that also name the scan.
func offset(v *tree.ViewNode, idx int64) {
	if v.Ref >= 0 {
		v.Ref = encode(idx, tree.ID(v.Ref))
	}
	for _, k := range v.Kids {
		offset(k, idx)
	}
}

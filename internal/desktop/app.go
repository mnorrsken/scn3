// Package desktop is the API the UI calls. It owns the running scans and
// turns tree views into what the chart needs. Only main.go wires it to Wails.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/mnorrsken/scn3/internal/platform"
	"github.com/mnorrsken/scn3/internal/scanner"
	"github.com/mnorrsken/scn3/internal/tree"
)

// Extra segment kinds next to the ones in package tree.
const (
	KindFree      = "free"      // free space on the volume
	KindUnscanned = "unscanned" // used on the volume but not found by the scan
)

// SummaryRef is the ref of the view that combines all volumes.
const SummaryRef int64 = -2

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
}

// App is bound to the webview. Methods are called from the UI.
type App struct {
	emit func(event string, data any)

	mu     sync.Mutex
	nextID int64
	byIdx  map[int64]*session
	byPath map[string]*session
}

// New returns an App that sends events through emit.
func New(emit func(event string, data any)) *App {
	return &App{emit: emit, byIdx: map[int64]*session{}, byPath: map[string]*session{}}
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
	if err := platform.Trash(s.scan.Tree.Path(id)); err != nil {
		return err
	}
	s.scan.Remove(id)
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
	if ref == SummaryRef {
		return a.summary(maxDepth, minFrac)
	}
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
	v := t.View(id, maxDepth, int64(minFrac*float64(span)))
	if id == tree.Root {
		v.Name = s.name
	}
	offset(v, s.idx)
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

// summary combines every volume into one view. Volumes that have not been
// scanned yet count as "not scanned". The UI starts their scans.
func (a *App) summary(maxDepth int, minFrac float64) (*View, error) {
	vols, err := platform.Volumes()
	if err != nil {
		return nil, err
	}
	var capacity, free, used, scanned, files int64
	denied := 0
	scanning := false
	type part struct {
		s    *session
		size int64
	}
	var parts []part
	for _, vol := range vols {
		capacity += vol.Capacity
		free += vol.Free
		used += vol.Capacity - vol.Free
		a.mu.Lock()
		s := a.byPath[filepath.Clean(vol.Path)]
		running := s != nil && s.running
		a.mu.Unlock()
		if s == nil {
			continue
		}
		size, f := s.scan.Tree.Size(tree.Root)
		scanned += size
		files += f
		denied += s.scan.Tree.Denied()
		scanning = scanning || running
		parts = append(parts, part{s, size})
	}

	root := &tree.ViewNode{Ref: SummaryRef, Name: "All volumes", Size: scanned, Files: files, Kind: tree.KindDir}
	span := max(capacity, scanned+free)
	minBytes := int64(minFrac * float64(span))
	for _, p := range parts {
		if p.size == 0 {
			continue
		}
		k := p.s.scan.Tree.View(tree.Root, maxDepth-1, minBytes)
		k.Name = p.s.name
		offset(k, p.s.idx)
		root.Kids = append(root.Kids, k)
	}
	sortBySize(root.Kids)
	addVolumeArcs(root, used-scanned, free)
	return &View{
		Root:        root,
		Crumbs:      []tree.Crumb{{Ref: SummaryRef, Name: root.Name}},
		Parent:      -1,
		Sep:         string(filepath.Separator),
		VolumeBytes: span,
		Scanning:    scanning,
		Denied:      denied,
	}, nil
}

func sortBySize(kids []*tree.ViewNode) {
	for i := 1; i < len(kids); i++ {
		for j := i; j > 0 && kids[j-1].Size < kids[j].Size; j-- {
			kids[j-1], kids[j] = kids[j], kids[j-1]
		}
	}
}

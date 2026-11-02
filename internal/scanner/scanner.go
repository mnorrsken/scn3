// Package scanner walks a directory tree in parallel and fills a tree.Tree.
//
// Worker goroutines only read directories. One collector goroutine owns the
// tree writes and the hard-link table, so neither needs fine-grained locking.
// Totals are updated as results arrive, so a view taken during a scan shows
// the partial tree.
package scanner

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mnorrsken/scn3/internal/platform"
	"github.com/mnorrsken/scn3/internal/tree"
)

// Progress is a snapshot of a running scan.
type Progress struct {
	Dirs    int64         `json:"dirs"`
	Files   int64         `json:"files"`
	Bytes   int64         `json:"bytes"`
	Errors  int64         `json:"errors"`
	Elapsed time.Duration `json:"elapsed"`
	Done    bool          `json:"done"`
}

type fileKey struct{ dev, ino uint64 }

// Scan is the tree of one root path plus what is needed to rescan parts of it.
// Run, Rescan and Remove must not overlap.
type Scan struct {
	Tree *tree.Tree
	Root string

	boundary *platform.Boundary
	seen     map[fileKey]tree.ID // hard-linked files, and the directory that counted each

	// ProgressEvery is how often Run reports progress.
	ProgressEvery time.Duration
	// Workers is the number of parallel directory readers.
	Workers int
}

// New prepares a scan of root, which must be a directory.
func New(root string) (*Scan, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", root, err)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	e, err := platform.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", abs, err)
	}
	if !e.IsDir {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	t := tree.New(abs)
	t.SetOwn(tree.Root, e.Size)
	workers := 2 * runtime.NumCPU()
	if workers < 4 {
		workers = 4
	} else if workers > 32 {
		workers = 32
	}
	return &Scan{
		Tree:          t,
		Root:          abs,
		boundary:      platform.NewBoundary(e, abs),
		seen:          map[fileKey]tree.ID{},
		ProgressEvery: 100 * time.Millisecond,
		Workers:       workers,
	}, nil
}

// Run scans the whole root.
func (s *Scan) Run(ctx context.Context, onProgress func(Progress)) error {
	return s.walk(ctx, tree.Root, s.Root, onProgress)
}

// Rescan scans directory id again and patches the result into the tree.
func (s *Scan) Rescan(ctx context.Context, id tree.ID, onProgress func(Progress)) error {
	path := s.Tree.Path(id)
	e, err := platform.Stat(path)
	if err != nil {
		// Gone from disk: drop it, as a full rescan would.
		if id != tree.Root {
			s.Remove(id)
			return nil
		}
		return fmt.Errorf("rescanning %s: %w", path, err)
	}
	s.forget(id)
	s.Tree.Clear(id)
	s.Tree.SetOwn(id, e.Size)
	return s.walk(ctx, id, path, onProgress)
}

// Remove takes directory id out of the tree, for example after moving it to
// the trash. A hard link to a file inside it that lives elsewhere is not
// recounted until that other directory is rescanned.
func (s *Scan) Remove(id tree.ID) {
	s.forget(id)
	s.Tree.Remove(id)
}

// forget clears hard-link records owned by id or its descendants, so a rescan
// counts those files again.
func (s *Scan) forget(id tree.ID) {
	for k, owner := range s.seen {
		if s.Tree.IsUnder(owner, id) {
			delete(s.seen, k)
		}
	}
}

type job struct {
	id   tree.ID
	path string
}

type linkedFile struct {
	key  fileKey
	size int64
}

type result struct {
	id     tree.ID
	path   string
	bytes  int64
	files  uint32
	kids   []tree.Child
	linked []linkedFile
	err    error
}

// stack is an unbounded LIFO of directories. Going depth first keeps it small.
type stack struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []job
	closed bool
}

func newStack() *stack {
	s := &stack{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *stack) push(j job) {
	s.mu.Lock()
	s.items = append(s.items, j)
	s.mu.Unlock()
	s.cond.Signal()
}

func (s *stack) pop() (job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.items) == 0 && !s.closed {
		s.cond.Wait()
	}
	if s.closed {
		return job{}, false
	}
	j := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return j, true
}

func (s *stack) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *Scan) read(j job) result {
	r := result{id: j.id, path: j.path}
	entries, err := platform.ReadDir(j.path)
	r.err = err
	for _, e := range entries {
		switch {
		case e.IsDir:
			if s.boundary.Follow(e, filepath.Join(j.path, e.Name)) {
				r.kids = append(r.kids, tree.Child{Name: e.Name, Own: e.Size})
			}
		case e.Linked:
			r.linked = append(r.linked, linkedFile{fileKey{e.Dev, e.Ino}, e.Size})
		default:
			r.bytes += e.Size
			r.files++
		}
	}
	return r
}

func (s *Scan) walk(ctx context.Context, start tree.ID, path string, onProgress func(Progress)) error {
	began := time.Now()
	jobs := newStack()
	results := make(chan result, s.Workers)

	var wg sync.WaitGroup
	for range s.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				j, ok := jobs.pop()
				if !ok {
					return
				}
				select {
				case results <- s.read(j):
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	var dirs, files, bytes, errs atomic.Int64
	snapshot := func(done bool) Progress {
		return Progress{
			Dirs: dirs.Load(), Files: files.Load(), Bytes: bytes.Load(), Errors: errs.Load(),
			Elapsed: time.Since(began), Done: done,
		}
	}

	stopTicker := make(chan struct{})
	var tickerDone sync.WaitGroup
	if onProgress != nil {
		tickerDone.Add(1)
		go func() {
			defer tickerDone.Done()
			t := time.NewTicker(s.ProgressEvery)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					onProgress(snapshot(false))
				case <-stopTicker:
					return
				}
			}
		}()
	}

	jobs.push(job{id: start, path: path})
	pending := 1
	var err error
	for pending > 0 && err == nil {
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case r := <-results:
			pending--
			for _, f := range r.linked {
				if _, dup := s.seen[f.key]; dup {
					continue
				}
				s.seen[f.key] = r.id
				r.bytes += f.size
				r.files++
			}
			first := s.Tree.Add(r.id, r.bytes, r.files, r.kids, r.err != nil)
			for i, k := range r.kids {
				jobs.push(job{id: first + tree.ID(i), path: filepath.Join(r.path, k.Name)})
				bytes.Add(k.Own)
			}
			pending += len(r.kids)
			dirs.Add(1)
			files.Add(int64(r.files))
			bytes.Add(r.bytes)
			if r.err != nil {
				errs.Add(1)
			}
		}
	}

	jobs.close()
	// Free workers blocked on a full results channel when the scan was cancelled.
	go func() {
		wg.Wait()
		close(results)
	}()
	for range results {
	}
	close(stopTicker)
	tickerDone.Wait()
	if onProgress != nil {
		onProgress(snapshot(true))
	}
	return err
}

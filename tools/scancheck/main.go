// Command scancheck finds what scn3's scan leaves out, and why.
//
// It compares two scans of the same folder. First, as your normal user:
//
//	go run ./tools/scancheck app <path> > app.jsonl
//
// This runs the app's own scanner and saves what it counted in each folder.
// Then, best as admin (sudo on macOS and Linux, an elevated terminal on Windows):
//
//	go run ./tools/scancheck compare <path> app.jsonl > report.md
//
// This walks the tree again with Go's standard library, a separate code path
// from the app's, and writes a Markdown report of every difference, each with
// a reason and whether the app is right to leave it out.
//
// It is a development tool, not part of any release.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mnorrsken/scn3/internal/platform"
	"github.com/mnorrsken/scn3/internal/scanner"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "scancheck:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	usage := errors.New("usage: scancheck app <path> > app.jsonl\n       scancheck compare [-top N] <path> app.jsonl > report.md")
	if len(args) == 0 {
		return usage
	}
	switch args[0] {
	case "app":
		if len(args) != 2 {
			return usage
		}
		return appScan(args[1], stdout, stderr)
	case "compare":
		fs := flag.NewFlagSet("compare", flag.ContinueOnError)
		fs.SetOutput(stderr)
		top := fs.Int("top", 50, "how many items to list per reason")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 2 {
			return usage
		}
		snap, err := loadSnapshot(fs.Arg(1))
		if err != nil {
			return err
		}
		return compare(fs.Arg(0), snap, *top, stdout, stderr)
	}
	return usage
}

// ---- the app's scan ----

type header struct {
	Root     string    `json:"root"`
	Elevated bool      `json:"elevated"`
	Time     time.Time `json:"time"`
}

type appDir struct {
	Path   string `json:"p"`
	Files  int64  `json:"f"`
	Bytes  int64  `json:"b"`
	Denied bool   `json:"d,omitempty"`
}

type snapshot struct {
	header
	dirs map[string]appDir
}

func appScan(path string, stdout, stderr io.Writer) error {
	s, err := scanner.New(path)
	if err != nil {
		return err
	}
	if elevated() {
		fmt.Fprintln(stderr, "scancheck: warning: running as admin; run the app scan as your normal user to see what it misses")
	}
	start := time.Now()
	if err := s.Run(context.Background(), nil); err != nil {
		return err
	}
	w := bufio.NewWriter(stdout)
	enc := json.NewEncoder(w)
	if err := enc.Encode(header{Root: s.Root, Elevated: elevated(), Time: start}); err != nil {
		return err
	}
	var werr error
	n := 0
	s.Tree.Walk(func(p string, bytes, files int64, denied bool) {
		if werr == nil {
			werr = enc.Encode(appDir{Path: p, Files: files, Bytes: bytes, Denied: denied})
			n++
		}
	})
	if werr != nil {
		return werr
	}
	fmt.Fprintf(stderr, "scancheck: %d folders scanned in %s\n", n, time.Since(start).Round(time.Millisecond))
	return w.Flush()
}

func loadSnapshot(file string) (*snapshot, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReader(f))
	snap := &snapshot{dirs: map[string]appDir{}}
	if err := dec.Decode(&snap.header); err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	for {
		var d appDir
		if err := dec.Decode(&d); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}
		snap.dirs[d.Path] = d
	}
	return snap, nil
}

// ---- the reference walk ----

// refDir is what the reference walk found directly in one folder.
type refDir struct {
	files, bytes int64 // all files, each counted
	dupFiles     int64 // of those, extra hard links of a file met before
	dupBytes     int64
	linked       int64 // files with more than one hard link
	subFiles     int64 // files in the whole subtree, without duplicates
	subBytes     int64
}

// skip is a folder the reference walk did not go into, or could not read.
type skip struct {
	path   string
	reason reason
	detail string
}

type walker struct {
	boundary *platform.Boundary
	dirs     map[string]*refDir
	skips    []skip
	seen     map[fileKey]bool
	progress func()
}

type fileKey struct{ dev, ino uint64 }

// entryInfo is what the reference walk learns about one listed item.
type entryInfo struct {
	isDir  bool
	size   int64 // bytes on disk
	dev    uint64
	key    fileKey
	links  uint32
	follow reason // for a folder: why not to enter it, or none
	detail string
}

func (w *walker) walk(path string) (files, bytes int64) {
	w.progress()
	entries, err := os.ReadDir(path)
	d := &refDir{}
	w.dirs[path] = d
	if err != nil {
		w.skips = append(w.skips, skip{path, reasonUnreadableAdmin, err.Error()})
		// os.ReadDir returns what it read before the error; count that too.
	}
	for _, e := range entries {
		p := filepath.Join(path, e.Name())
		info, err := inspect(p, e)
		if err != nil {
			d.files++ // listed, but its details could not be read
			continue
		}
		if info.isDir {
			if info.follow == reasonNone && !w.boundary.Follow(platform.Entry{Dev: info.dev}, p) {
				info.follow = reasonBoundary
			}
			if info.follow != reasonNone {
				w.skips = append(w.skips, skip{p, info.follow, info.detail})
				continue
			}
			f, b := w.walk(p)
			d.subFiles += f
			d.subBytes += b
			continue
		}
		d.files++
		d.bytes += info.size
		if info.links > 1 {
			d.linked++
			if w.seen[info.key] {
				d.dupFiles++
				d.dupBytes += info.size
				continue
			}
			w.seen[info.key] = true
		}
	}
	d.subFiles += d.files - d.dupFiles
	d.subBytes += d.bytes - d.dupBytes
	return d.subFiles, d.subBytes
}

// ---- comparing ----

type reason int

const (
	reasonNone reason = iota
	reasonBoundary
	reasonLink
	reasonOnlineOnly
	reasonDuplicate
	reasonNeedsAdmin
	reasonUnreadableAdmin
	reasonMissingFolder
	reasonListing
	reasonCount
	reasonOnlyInApp
	reasonSize
)

var reasons = map[reason]struct {
	title    string
	verdict  string
	priority int
}{
	reasonBoundary:        {"Another filesystem, mount point or skipped system folder", "Fine to ignore: on purpose, so nothing is counted twice", 9},
	reasonLink:            {"Symlink, junction or mounted drive", "Fine to ignore: it points at a place that is counted where it really is", 8},
	reasonOnlineOnly:      {"Cloud folder that exists only online", "Fine to ignore: it takes no space on this disk", 7},
	reasonDuplicate:       {"Hard link or APFS clone counted in another folder", "Fine to ignore: the data is on disk once, so it is counted once", 6},
	reasonNeedsAdmin:      {"Unreadable as you, readable as admin", "Needs a look: the app undercounts here; it needs more rights (admin, Full Disk Access)", 2},
	reasonUnreadableAdmin: {"Unreadable even as admin", "Judge for yourself: usually system-protected folders", 5},
	reasonMissingFolder:   {"Folder missing from the app scan, no known reason", "Needs a look: likely a scanner bug", 0},
	reasonListing:         {"Files the app's directory listing did not return", "Needs a look: likely a scanner bug", 1},
	reasonCount:           {"Other file count differences", "Needs a look, unless the folder changed between the two scans", 3},
	reasonOnlyInApp:       {"Folders only in the app scan", "Usually fine: deleted or unreadable between the two scans", 10},
	reasonSize:            {"Size differences of more than 10% and 1 MiB", "Usually fine: files changed between the scans, or compressed files", 4},
}

type finding struct {
	reason reason
	path   string
	files  int64
	bytes  int64
	detail string
}

func compare(path string, snap *snapshot, top int, stdout, stderr io.Writer) error {
	root, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if root != snap.Root {
		return fmt.Errorf("the app scan was of %s, not %s", snap.Root, root)
	}
	rootEntry, err := platform.Stat(root)
	if err != nil {
		return err
	}
	last := time.Now()
	count := 0
	w := &walker{
		boundary: platform.NewBoundary(rootEntry, root),
		dirs:     map[string]*refDir{},
		seen:     map[fileKey]bool{},
		progress: func() {
			count++
			if time.Since(last) > 5*time.Second {
				fmt.Fprintf(stderr, "scancheck: %d folders walked\n", count)
				last = time.Now()
			}
		},
	}
	start := time.Now()
	refFiles, refBytes := w.walk(root)
	took := time.Since(start)

	var out []finding
	for _, s := range w.skips {
		out = append(out, finding{reason: s.reason, path: s.path, detail: s.detail})
	}
	for p, d := range w.dirs {
		a, inApp := snap.dirs[p]
		if !inApp {
			parent, parentInApp := snap.dirs[filepath.Dir(p)]
			if _, parentWalked := w.dirs[filepath.Dir(p)]; parentWalked && !parentInApp {
				continue // counted with its topmost missing parent
			}
			r := reasonMissingFolder
			if parentInApp && parent.Denied {
				r = reasonNeedsAdmin
			}
			out = append(out, finding{reason: r, path: p, files: d.subFiles, bytes: d.subBytes})
			continue
		}
		want := d.files - d.dupFiles
		diff := want - a.Files
		switch {
		case diff > 0 && a.Denied:
			out = append(out, finding{reasonNeedsAdmin, p, diff, max(d.bytes-d.dupBytes-a.Bytes, 0), "folder partly unreadable as you"})
		case diff != 0:
			out = append(out, explain(p, d, diff, a.Bytes))
		}
		if gap := d.bytes - d.dupBytes - a.Bytes; diff == 0 && abs(gap) > 1<<20 && abs(gap)*10 > max(a.Bytes, 1) {
			out = append(out, finding{reasonSize, p, 0, gap, fmt.Sprintf("app %s, reference %s", human(a.Bytes), human(d.bytes-d.dupBytes))})
		}
	}
	for p, a := range snap.dirs {
		if _, walked := w.dirs[p]; !walked {
			if _, parentInApp := snap.dirs[filepath.Dir(p)]; parentInApp || p == root {
				if _, parentWalked := w.dirs[filepath.Dir(p)]; parentWalked {
					out = append(out, finding{reasonOnlyInApp, p, a.Files, a.Bytes, ""})
				}
			}
		}
	}
	writeReport(stdout, snap, root, refFiles, refBytes, took, out, top)
	return nil
}

// explain looks at a folder where the app counted a different number of files
// than the reference walk, by listing it again the app's way.
func explain(path string, d *refDir, diff, appBytes int64) finding {
	appList, err := platform.ReadDir(path)
	osList, _ := os.ReadDir(path)
	appNames := map[string]bool{}
	clones := int64(0)
	for _, e := range appList {
		appNames[e.Name] = true
		if e.Linked {
			clones++
		}
	}
	var missing []string
	for _, e := range osList {
		if !appNames[e.Name()] {
			missing = append(missing, e.Name())
		}
	}
	if len(missing) > 0 {
		detail := strings.Join(firstN(missing, 10), ", ")
		if err != nil {
			detail += " (listing error: " + err.Error() + ")"
		}
		return finding{reasonListing, path, int64(len(missing)), 0, detail}
	}
	// A hard-linked or cloned file is counted once, but the app and this walk
	// may meet it first in different folders.
	if linked := max(clones, d.linked); abs(diff) <= linked {
		where := "counted in another folder by the app"
		if diff < 0 {
			where = "counted here by the app, in another folder by the reference walk"
		}
		return finding{reasonDuplicate, path, abs(diff), 0, fmt.Sprintf("%d hard-linked or cloned files here, %s", linked, where)}
	}
	if diff < 0 {
		return finding{reasonCount, path, diff, appBytes - (d.bytes - d.dupBytes), "the app counted more files"}
	}
	return finding{reasonCount, path, diff, 0, fmt.Sprintf("reference %d files, app %d", d.files-d.dupFiles, d.files-d.dupFiles-diff)}
}

// ---- report ----

func writeReport(w io.Writer, snap *snapshot, root string, files, bytes int64, took time.Duration, out []finding, top int) {
	appFiles, appBytes := int64(0), int64(0)
	for _, d := range snap.dirs {
		appFiles += d.Files
		appBytes += d.Bytes
	}
	fmt.Fprintf(w, "# Scan check: %s\n\n", root)
	fmt.Fprintf(w, "| | Files | Size |\n| --- | --- | --- |\n")
	fmt.Fprintf(w, "| App scan (%s, %s) | %d | %s |\n", who(snap.Elevated), snap.Time.Format("2006-01-02 15:04"), appFiles, human(appBytes))
	fmt.Fprintf(w, "| Reference walk (%s, took %s) | %d | %s |\n", who(elevated()), took.Round(time.Second), files, human(bytes))
	fmt.Fprintf(w, "\n")
	if runtime.GOOS == "darwin" {
		fmt.Fprintf(w, "The reference walk counts each hard link once, like the app, but APFS clones in full, so its total can be higher.\n")
	}
	if !elevated() {
		fmt.Fprintf(w, "It did not run as admin, so folders unreadable to you are unreadable to it too.\n")
		r := reasons[reasonUnreadableAdmin]
		r.title, r.verdict = "Unreadable (compare was not run as admin)", "Run compare as admin to see whether the app misses data here"
		reasons[reasonUnreadableAdmin] = r
	}

	by := map[reason][]finding{}
	for _, f := range out {
		by[f.reason] = append(by[f.reason], f)
	}
	var order []reason
	for r := range by {
		order = append(order, r)
	}
	sort.Slice(order, func(i, j int) bool { return reasons[order[i]].priority < reasons[order[j]].priority })

	fmt.Fprintf(w, "\n## Summary\n\n| Reason | Verdict | Items | Files | Size |\n| --- | --- | --- | --- | --- |\n")
	for _, r := range order {
		var f, b int64
		for _, x := range by[r] {
			f += x.files
			b += x.bytes
		}
		fmt.Fprintf(w, "| %s | %s | %d | %s | %s |\n", reasons[r].title, reasons[r].verdict, len(by[r]), orDash(f), humanOrDash(b))
	}
	if len(order) == 0 {
		fmt.Fprintf(w, "\nNo differences.\n")
	}
	for _, r := range order {
		list := by[r]
		sort.Slice(list, func(i, j int) bool {
			if abs(list[i].bytes) != abs(list[j].bytes) {
				return abs(list[i].bytes) > abs(list[j].bytes)
			}
			if list[i].files != list[j].files {
				return list[i].files > list[j].files
			}
			return list[i].path < list[j].path
		})
		fmt.Fprintf(w, "\n## %s\n\n%s.\n\n| Path | Files | Size | Detail |\n| --- | --- | --- | --- |\n", reasons[r].title, reasons[r].verdict)
		for _, f := range firstN(list, top) {
			fmt.Fprintf(w, "| `%s` | %s | %s | %s |\n", f.path, orDash(f.files), humanOrDash(f.bytes), strings.ReplaceAll(f.detail, "|", "\\|"))
		}
		if len(list) > top {
			fmt.Fprintf(w, "\n%d more not shown (use -top).\n", len(list)-top)
		}
	}
}

func who(admin bool) string {
	if admin {
		return "as admin"
	}
	return "as user"
}

func firstN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func orDash(n int64) string {
	if n == 0 {
		return "–"
	}
	return fmt.Sprint(n)
}

func humanOrDash(n int64) string {
	if n == 0 {
		return "–"
	}
	return human(n)
}

func human(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v, i := float64(n), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%s%d B", sign, n)
	}
	return fmt.Sprintf("%s%.1f %s", sign, v, units[i])
}

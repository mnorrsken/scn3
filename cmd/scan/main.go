// Command scan measures a directory tree and prints the result.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"

	"github.com/mnorrsken/scn3/internal/scanner"
	"github.com/mnorrsken/scn3/internal/tree"
	"github.com/mnorrsken/scn3/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the tree as JSON")
	depth := fs.Int("depth", 1<<30, "with --json, how many levels below the root to print")
	showVersion := fs.Bool("version", false, "print the version")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: scan <path> [--json] [--depth N]")
		fs.PrintDefaults()
	}

	// Accept flags after the path too: scan <path> --json.
	var paths []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		paths = append(paths, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if *showVersion {
		fmt.Fprintf(stdout, "scan %s (%s)\n", version.Version, version.Commit)
		return nil
	}
	if len(paths) != 1 {
		fs.Usage()
		return fmt.Errorf("need exactly one path")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	s, err := scanner.New(paths[0])
	if err != nil {
		return err
	}
	var last scanner.Progress
	if err := s.Run(ctx, func(p scanner.Progress) { last = p }); err != nil {
		return err
	}
	if last.Errors > 0 {
		fmt.Fprintf(stderr, "scan: %d directories could not be read fully\n", last.Errors)
		for _, err := range s.ReadErrors() {
			fmt.Fprintln(stderr, "scan:", err)
		}
	}

	if *asJSON {
		w := bufio.NewWriter(stdout)
		v := s.Tree.View(tree.Root, *depth, 0)
		v.Name = filepath.Base(s.Root)
		if err := json.NewEncoder(w).Encode(jsonNode(v)); err != nil {
			return fmt.Errorf("writing JSON: %w", err)
		}
		return w.Flush()
	}

	size, files := s.Tree.Size(tree.Root)
	fmt.Fprintf(stdout, "%s: %d bytes, %d files, %d directories, %s\n", s.Root, size, files, last.Dirs, last.Elapsed.Round(1e6))
	kids := s.Tree.View(tree.Root, 1, 0).Kids
	sort.SliceStable(kids, func(a, b int) bool { return kids[a].Size > kids[b].Size })
	for _, k := range kids {
		fmt.Fprintf(stdout, "%14d  %s\n", k.Size, k.Name)
	}
	return nil
}

// outNode is the public JSON shape: no internal IDs.
type outNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path,omitempty"`
	Kind     string     `json:"kind"`
	Size     int64      `json:"size"`
	Files    int64      `json:"files"`
	Denied   bool       `json:"denied,omitempty"`
	Children []*outNode `json:"children,omitempty"`
}

func jsonNode(v *tree.ViewNode) *outNode {
	n := &outNode{Name: v.Name, Path: v.Path, Kind: v.Kind, Size: v.Size, Files: v.Files, Denied: v.Denied}
	for _, k := range v.Kids {
		n.Children = append(n.Children, jsonNode(k))
	}
	return n
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHardLinkIsExplainedAndNothingNeedsALook(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "f"), make([]byte, 100_000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "a", "f"), filepath.Join(root, "b", "f")); err != nil {
		t.Skip("no hard links here:", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
	}

	var app, report, log bytes.Buffer
	if err := run([]string{"app", root}, &app, &log); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "app.jsonl")
	if err := os.WriteFile(file, app.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"compare", root, file}, &report, &log); err != nil {
		t.Fatal(err)
	}
	got := report.String()
	if strings.Contains(got, "Needs a look") {
		t.Fatalf("a hard link or symlink was reported as a problem:\n%s", got)
	}
	if !strings.Contains(got, "No differences.") && !strings.Contains(got, reasons[reasonDuplicate].title) {
		t.Fatalf("unexpected report:\n%s", got)
	}
}

func TestCompareRefusesAnotherRoot(t *testing.T) {
	root := t.TempDir()
	var app, out, log bytes.Buffer
	if err := run([]string{"app", root}, &app, &log); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "app.jsonl")
	if err := os.WriteFile(file, app.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"compare", t.TempDir(), file}, &out, &log); err == nil {
		t.Fatal("compared against a scan of another folder")
	}
}

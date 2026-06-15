package vault

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTreeShowsRealTreeMinusIgnores(t *testing.T) {
	root := t.TempDir()
	v, err := OpenSource(root)
	if err != nil {
		t.Fatal(err)
	}
	// A real-ish project: source, extension-less and odd files, plus dependency,
	// build, and VCS trees that must never show.
	mustWriteFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "README"), "hi\n")                   // no extension
	mustWriteFile(t, filepath.Join(root, "Makefile"), "all:\n")               // no extension
	mustWriteFile(t, filepath.Join(root, ".gitignore"), "dist/\n")            // a real project file
	mustWriteFile(t, filepath.Join(root, "dist", "out.js"), "x\n")            // gitignored
	mustWriteFile(t, filepath.Join(root, "node_modules", "p", "i.js"), "x\n") // default ignore
	mustWriteFile(t, filepath.Join(root, ".git", "config"), "x\n")            // default ignore

	nodes, truncated, err := v.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("small tree should not be truncated")
	}
	got := map[string]bool{}
	for _, n := range nodes {
		got[n.RelPath] = true
	}
	for _, want := range []string{"src", "src/main.go", "README", "Makefile", ".gitignore"} {
		if !got[want] {
			t.Errorf("tree missing %q (real-tree files should show)", want)
		}
	}
	for _, no := range []string{"dist", "dist/out.js", "node_modules", "node_modules/p/i.js", ".git", ".git/config"} {
		if got[no] {
			t.Errorf("tree should have ignored %q", no)
		}
	}
}

func TestOpenSourceRejectsMissingAndFile(t *testing.T) {
	if _, err := OpenSource(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("OpenSource on a missing dir should error")
	}
	f := filepath.Join(t.TempDir(), "file.txt")
	mustWriteFile(t, f, "x\n")
	if _, err := OpenSource(f); err == nil {
		t.Error("OpenSource on a file should error")
	}
}

func TestReadSourceBinaryPlaceholder(t *testing.T) {
	root := t.TempDir()
	v, _ := OpenSource(root)
	mustWriteFile(t, filepath.Join(root, "logo.png"), "\x89PNG\x00\x00binary")
	n, err := v.ReadSource("logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if n.Source != "code" {
		t.Errorf("Source = %q, want code (read-only)", n.Source)
	}
	if !strings.Contains(n.Body, "binary file") {
		t.Errorf("binary body should be a placeholder, got %q", n.Body)
	}
}

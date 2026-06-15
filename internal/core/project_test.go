package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dcode/internal/config"
	"dcode/internal/tutor"
)

func TestOpenProjectAndReopenKeepNotesSeparate(t *testing.T) {
	base := t.TempDir()
	notesBase := filepath.Join(base, "notes")
	projA := filepath.Join(base, "projA")
	projB := filepath.Join(base, "projB")
	mustMkFile(t, filepath.Join(projA, "a.go"), "package a\n")
	mustMkFile(t, filepath.Join(projB, "b.go"), "package b\n")

	tut := tutor.New(config.AIConfig{Provider: "openai"})

	a, err := OpenProject(projA, notesBase, tut, false)
	if err != nil {
		t.Fatalf("OpenProject A: %v", err)
	}
	if a.ProjectName() != "projA" {
		t.Fatalf("ProjectName = %q, want projA", a.ProjectName())
	}
	// Save a note while project A is open.
	if _, err := a.SaveNote("a.go.md", "# explains a\n"); err != nil {
		t.Fatalf("SaveNote in A: %v", err)
	}

	// Switch to project B; A's note must not appear in B's tree.
	b, err := a.Reopen(projB, notesBase)
	if err != nil {
		t.Fatalf("Reopen B: %v", err)
	}
	if b.ProjectName() != "projB" {
		t.Fatalf("after reopen ProjectName = %q, want projB", b.ProjectName())
	}
	tree, _, err := b.Tree()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range tree {
		if e.Path == "a.go.md" || e.Path == "a.go" {
			t.Fatalf("project B's tree leaked project A content: %+v", tree)
		}
	}

	// Reopening A brings its note back (per-project notes persisted on disk).
	a2, err := b.Reopen(projA, notesBase)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := a2.OpenNote("a.go.md"); err != nil || !strings.Contains(n.Body, "explains a") {
		t.Fatalf("project A's note did not persist: note=%+v err=%v", n, err)
	}
}

func TestProjectNotesDirIsStableAndDistinct(t *testing.T) {
	base := "/notes"
	one := ProjectNotesDir(base, "/home/u/projA")
	two := ProjectNotesDir(base, "/home/u/projB")
	if one == two {
		t.Fatal("different projects must get different notes dirs")
	}
	if one != ProjectNotesDir(base, "/home/u/projA") {
		t.Fatal("notes dir must be stable for the same project")
	}
}

func TestOpenProjectMissingPathErrors(t *testing.T) {
	tut := tutor.New(config.AIConfig{Provider: "openai"})
	if _, err := OpenProject(filepath.Join(t.TempDir(), "nope"), t.TempDir(), tut, false); err == nil {
		t.Fatal("opening a missing project (createCode=false) should error")
	}
}

func mustMkFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

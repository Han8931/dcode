package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dcode/internal/config"
	"dcode/internal/tutor"
	"dcode/internal/vault"
)

// explainService seeds a source root with the given files and returns a Service.
func explainService(t *testing.T, files map[string]string) (*Service, string, string) {
	t.Helper()
	codeDir := t.TempDir()
	notesDir := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(codeDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, err := vault.Open(codeDir)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(notesDir)
	if err != nil {
		t.Fatal(err)
	}
	return New(code, notes, tutor.New(config.AIConfig{Provider: "openai"})), codeDir, notesDir
}

func TestProjectContextGathersSiblings(t *testing.T) {
	s, _, _ := explainService(t, map[string]string{
		"pkg/a.go":     "package pkg\n\nfunc A() {}\n",
		"pkg/b.go":     "package pkg\n\nfunc B() {}\n",
		"other/c.go":   "package other\n\nfunc C() {}\n",
		"pkg/notes.md": "# not source\n",
	})
	ctx := s.projectContext("pkg/a.go")
	// The sibling's contents are included (same package/dir).
	if !strings.Contains(ctx, "pkg/b.go") || !strings.Contains(ctx, "func B") {
		t.Fatalf("sibling pkg/b.go should be in project context:\n%s", ctx)
	}
	// The project file map lists every source path, including other dirs.
	if !strings.Contains(ctx, "Project source files:") || !strings.Contains(ctx, "other/c.go") {
		t.Fatalf("project file map should list all source paths:\n%s", ctx)
	}
	// But only directory neighbours' CONTENTS are inlined — not the target's own
	// body, and not files from other directories.
	if strings.Contains(ctx, "func A") {
		t.Fatal("the target file's own body should not be inlined as context")
	}
	if strings.Contains(ctx, "func C") {
		t.Fatal("other directories' contents should not be inlined")
	}
	// Markdown is never treated as source context.
	if strings.Contains(ctx, "notes.md") {
		t.Fatal("markdown files should not appear in source context")
	}
}

func TestExplainStreamOfflineDegrades(t *testing.T) {
	s, _, _ := explainService(t, map[string]string{"a.go": "package main\nfunc main(){}\n"})
	var got strings.Builder
	out, err := s.ExplainStream(context.Background(),
		ExplainRequest{Path: "a.go", Lang: "go"},
		func(d string) { got.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if out == "" || !strings.Contains(strings.ToLower(out), "offline") {
		t.Fatalf("offline explain should explain itself, got: %q", out)
	}
	if got.String() != out {
		t.Fatal("onDelta should receive the full offline message")
	}
}

func TestSaveExplanationMirrorsPathIntoNotesRoot(t *testing.T) {
	s, codeDir, notesDir := explainService(t, map[string]string{"pkg/foo.go": "package pkg\n"})
	meta, err := s.SaveExplanation("pkg/foo.go", "It defines package pkg.")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Path != "pkg/foo.go.md" {
		t.Fatalf("explanation note path = %q, want pkg/foo.go.md", meta.Path)
	}
	// It lands in the notes dir, never the source dir.
	if _, err := os.Stat(filepath.Join(notesDir, "pkg", "foo.go.md")); err != nil {
		t.Fatalf("note not in notes dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codeDir, "pkg", "foo.go.md")); err == nil {
		t.Fatal("explanation leaked into the source dir")
	}
	// And it's readable back through the service (routes to the notes root).
	n, err := s.OpenNote("pkg/foo.go.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Body, "It defines package pkg") {
		t.Fatalf("saved explanation body wrong: %q", n.Body)
	}
}

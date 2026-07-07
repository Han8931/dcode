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

// newTestService builds a Service over a temp source root + temp notes root and
// an offline AI client (no API key, non-Ollama provider => setup guidance replies).
func newTestService(t *testing.T) *Service {
	t.Helper()
	code, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tut := tutor.New(config.AIConfig{Provider: "openai"})
	if !tut.Offline() {
		t.Fatal("expected offline tutor for the test")
	}
	return New(code, notes, tut)
}

func TestSaveOpenList(t *testing.T) {
	s := newTestService(t)

	meta, err := s.SaveNote("history/Cold War.md", "# Cold War\n\nA rivalry.\n")
	if err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	if meta.Title != "Cold War" {
		t.Fatalf("derived title = %q", meta.Title)
	}

	n, err := s.OpenNote("history/Cold War.md")
	if err != nil {
		t.Fatalf("OpenNote: %v", err)
	}
	if n.Source != "user" || n.Body == "" {
		t.Fatalf("opened note wrong: %+v", n)
	}

	list, err := s.ListNotes()
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(list) != 1 || list[0].Path != "history/Cold War.md" {
		t.Fatalf("ListNotes = %+v", list)
	}
}

func TestBacklinks(t *testing.T) {
	s := newTestService(t)
	if _, err := s.SaveNote("Limits.md", "# Limits\n\nfoundational.\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveNote("Derivatives.md", "# Derivatives\n\nbuilds on [[Limits]].\n"); err != nil {
		t.Fatal(err)
	}

	back, err := s.Backlinks("Limits.md")
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 || back[0].Title != "Derivatives" {
		t.Fatalf("Backlinks = %+v", back)
	}

	// A note nobody links to has no backlinks.
	none, err := s.Backlinks("Derivatives.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no backlinks, got %+v", none)
	}
}

func TestSearch(t *testing.T) {
	s := newTestService(t)
	_, _ = s.SaveNote("math/Algebra.md", "# Algebra\n\nsolving equations\n")
	_, _ = s.SaveNote("bio/Cells.md", "# Cells\n\nmitochondria\n")

	hits, err := s.Search("equations")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "Algebra" {
		t.Fatalf("body search = %+v", hits)
	}
	if all, _ := s.Search(""); len(all) != 2 {
		t.Fatalf("empty query should return all, got %d", len(all))
	}
}

func TestSourceFilesAreReadOnlyAndNotesStaySeparate(t *testing.T) {
	codeDir := t.TempDir()
	notesDir := t.TempDir()
	const src = "package pkg\n\nfunc Foo() {}\n"
	if err := os.MkdirAll(filepath.Join(codeDir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codeDir, "pkg", "foo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := vault.Open(codeDir)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(notesDir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(code, notes, tutor.New(config.AIConfig{Provider: "openai"}))

	// The source file opens read-only, with its whole content as the body.
	n, err := s.OpenNote("pkg/foo.go")
	if err != nil {
		t.Fatalf("OpenNote source: %v", err)
	}
	if n.Source != "code" || !strings.Contains(n.Body, "func Foo") {
		t.Fatalf("source note wrong: %+v", n)
	}

	// Writing to a source path is rejected and the file on disk is untouched.
	if _, err := s.SaveNote("pkg/foo.go", "tampered"); err == nil {
		t.Fatal("writing to read-only source should be rejected")
	}
	if raw, _ := os.ReadFile(filepath.Join(codeDir, "pkg", "foo.go")); string(raw) != src {
		t.Fatal("source file was modified")
	}

	// A note mirroring the source path lands in the NOTES dir, never the code dir.
	if _, err := s.SaveNote("pkg/foo.go.md", "# explanation\n"); err != nil {
		t.Fatalf("SaveNote note: %v", err)
	}
	if _, err := os.Stat(filepath.Join(notesDir, "pkg", "foo.go.md")); err != nil {
		t.Fatalf("note not written to notes dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codeDir, "pkg", "foo.go.md")); err == nil {
		t.Fatal("note leaked into the source dir")
	}

	// The tree shows both, in one shared namespace.
	tree, _, err := s.Tree()
	if err != nil {
		t.Fatal(err)
	}
	var sawSrc, sawNote bool
	for _, e := range tree {
		switch e.Path {
		case "pkg/foo.go":
			sawSrc = true
		case "pkg/foo.go.md":
			sawNote = true
		}
	}
	if !sawSrc || !sawNote {
		t.Fatalf("tree missing source or note: %+v", tree)
	}
}

// TestRealTreeShowsNonSourceFilesReadOnly locks in the phase-1 behavior: the tree
// shows every project file (not just an extension allowlist), and a non-source
// file like a Makefile opens read-only and rejects writes.
func TestRealTreeShowsNonSourceFilesReadOnly(t *testing.T) {
	codeDir := t.TempDir()
	notesDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(codeDir, "Makefile"), []byte("all:\n\techo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := vault.OpenSource(codeDir)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(notesDir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(code, notes, tutor.New(config.AIConfig{Provider: "openai"}))

	tree, _, err := s.Tree()
	if err != nil {
		t.Fatal(err)
	}
	var sawMakefile bool
	for _, e := range tree {
		if e.Path == "Makefile" {
			sawMakefile = true
		}
	}
	if !sawMakefile {
		t.Fatalf("real tree should show Makefile: %+v", tree)
	}

	n, err := s.OpenNote("Makefile")
	if err != nil {
		t.Fatalf("OpenNote Makefile: %v", err)
	}
	if n.Source != "code" || !strings.Contains(n.Body, "echo hi") {
		t.Fatalf("Makefile should open read-only with its content: %+v", n)
	}
	if _, err := s.SaveNote("Makefile", "tampered"); err == nil {
		t.Fatal("writing to a non-source project file should be rejected")
	}
}

func TestChatOffline(t *testing.T) {
	s := newTestService(t)
	reply, err := s.Chat(context.Background(), "", []tutor.ChatTurn{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if reply == "" {
		t.Fatal("expected an offline chat reply")
	}
}

func TestTrimTurnsAndClampContext(t *testing.T) {
	long := make([]tutor.ChatTurn, 40)
	for i := range long {
		long[i] = tutor.ChatTurn{Role: "user", Content: "turn"}
	}
	if got := len(TrimTurns(long)); got != maxChatTurns {
		t.Fatalf("trimmed to %d, want %d", got, maxChatTurns)
	}
	short := long[:3]
	if got := len(TrimTurns(short)); got != 3 {
		t.Fatalf("short history must pass through, got %d", got)
	}

	big := strings.Repeat("x", maxContextChars+500)
	clamped := ClampContext(big)
	if len([]rune(clamped)) > maxContextChars+50 {
		t.Fatalf("context not clamped: %d runes", len([]rune(clamped)))
	}
	if !strings.HasSuffix(clamped, "(truncated)") {
		t.Fatal("clamped context should be marked truncated")
	}
	if ClampContext("small") != "small" {
		t.Fatal("small context must pass through")
	}
}

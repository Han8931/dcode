package vault

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseNote_FrontmatterAndBody(t *testing.T) {
	raw := []byte("---\n" +
		"id: derivatives\n" +
		"title: Derivatives\n" +
		"subject: math\n" +
		"tags: [calculus, analysis]\n" +
		"source: ai-generated\n" +
		"---\n\n" +
		"# Derivatives\n\nA derivative measures change. [[Limits]] first.\n")

	n, err := ParseNote("Math/Derivatives.md", raw)
	if err != nil {
		t.Fatalf("ParseNote: %v", err)
	}
	if n.ID != "derivatives" || n.Title != "Derivatives" || n.Subject != "math" || n.Source != "ai-generated" {
		t.Fatalf("frontmatter fields wrong: %+v", n)
	}
	if !reflect.DeepEqual(n.Tags, []string{"calculus", "analysis"}) {
		t.Fatalf("tags = %v", n.Tags)
	}
	if !strings.HasPrefix(n.Body, "# Derivatives") {
		t.Fatalf("body should start with the heading, got %q", n.Body)
	}
}

func TestParseNote_NoFrontmatter(t *testing.T) {
	n, err := ParseNote("notes/loose.md", []byte("# Loose Note\n\njust text\n"))
	if err != nil {
		t.Fatalf("ParseNote: %v", err)
	}
	if n.Title != "Loose Note" {
		t.Fatalf("title should come from H1, got %q", n.Title)
	}
	if !strings.Contains(n.Body, "just text") {
		t.Fatalf("body lost: %q", n.Body)
	}
}

func TestParseNote_InvalidFrontmatterKeptInBody(t *testing.T) {
	// Hand-written headers like "tags:git" (no space after the colon) are a
	// YAML scalar, not a map. The note must still load — block kept in the
	// body, title from the filename — instead of failing the whole vault.
	raw := []byte("---\ntags:git\n---\n\nbranching basics\n")
	n, err := ParseNote("Git/Branching.md", raw)
	if err != nil {
		t.Fatalf("ParseNote: %v", err)
	}
	if n.Title != "Branching" {
		t.Fatalf("title should fall back to filename, got %q", n.Title)
	}
	if !strings.Contains(n.Body, "tags:git") || !strings.Contains(n.Body, "branching basics") {
		t.Fatalf("body should keep the whole file, got %q", n.Body)
	}
}

func TestParseNote_TitleFallsBackToFilename(t *testing.T) {
	n, err := ParseNote("x/My File.md", []byte("no heading here\n"))
	if err != nil {
		t.Fatalf("ParseNote: %v", err)
	}
	if n.Title != "My File" {
		t.Fatalf("title = %q, want filename-derived", n.Title)
	}
}

func TestMarshalRoundTrip_PreservesExtra(t *testing.T) {
	raw := []byte("---\n" +
		"id: spanish-subjunctive\n" +
		"title: The Subjunctive\n" +
		"subject: spanish\n" +
		"tags:\n  - grammar\n" +
		"srs:\n  ease: 2.5\n  interval: 6\n" +
		"---\n\nbody text with [[Ojalá|ojala]]\n")

	n, err := ParseNote("Spanish/The Subjunctive.md", raw)
	if err != nil {
		t.Fatalf("ParseNote: %v", err)
	}
	if n.Extra == nil || n.Extra["srs"] == nil {
		t.Fatalf("srs should be preserved in Extra, got %+v", n.Extra)
	}

	// Re-parse the marshaled form and confirm nothing important was lost.
	n2, err := ParseNote(n.RelPath, n.Marshal())
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if n2.ID != n.ID || n2.Title != n.Title || n2.Subject != n.Subject {
		t.Fatalf("round-trip changed known fields: %+v vs %+v", n2, n)
	}
	if !reflect.DeepEqual(n2.Tags, []string{"grammar"}) {
		t.Fatalf("round-trip tags = %v", n2.Tags)
	}
	if n2.Extra["srs"] == nil {
		t.Fatalf("round-trip dropped srs Extra block")
	}
}

func TestParseLinks(t *testing.T) {
	body := "See [[Limits]] and [[Chain Rule|the chain rule]], also [[Limits]] again.\n" +
		"Not a link: [single]. Empty [[]] ignored."
	got := ParseLinks(body)
	want := []Link{
		{Target: "Limits"},
		{Target: "Chain Rule", Alias: "the chain rule"},
		{Target: "Limits"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseLinks = %#v\nwant %#v", got, want)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Hello World":       "hello-world",
		"  Trim Me!!  ":     "trim-me",
		"C++ & Go":          "c-go",
		"":                  "untitled",
		"already-kebab-1.2": "already-kebab-1.2",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveRelPath(t *testing.T) {
	if got := DeriveRelPath("math", "Derivatives"); got != "math/Derivatives.md" {
		t.Errorf("with subject: %q", got)
	}
	if got := DeriveRelPath("", "Loose Note"); got != "Loose Note.md" {
		t.Errorf("no subject: %q", got)
	}
	if got := DeriveRelPath("a/b", "x:y"); got != "ab/xy.md" {
		t.Errorf("sanitized: %q", got)
	}
}

func TestVaultWriteReadList(t *testing.T) {
	dir := t.TempDir()
	v, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	written, err := v.Write(Note{
		Title:   "Photosynthesis",
		Subject: "biology",
		Source:  "ai-generated",
		Body:    "# Photosynthesis\n\nPlants make sugar. See [[Chlorophyll]].\n",
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written.ID == "" {
		t.Fatalf("Write should fill in an ID")
	}
	if written.Created == "" {
		t.Fatalf("Write should fill in Created")
	}
	if written.RelPath != "biology/Photosynthesis.md" {
		t.Fatalf("derived RelPath = %q", written.RelPath)
	}
	if _, err := filepath.Rel(dir, written.Path); err != nil {
		t.Fatalf("abs path not under root: %v", err)
	}

	got, err := v.Read(written.RelPath)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Title != "Photosynthesis" || got.Subject != "biology" {
		t.Fatalf("read-back metadata wrong: %+v", got)
	}
	if links := ParseLinks(got.Body); len(links) != 1 || links[0].Target != "Chlorophyll" {
		t.Fatalf("link not preserved: %v", links)
	}

	notes, err := v.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 1 || notes[0].RelPath != "biology/Photosynthesis.md" {
		t.Fatalf("List = %+v", notes)
	}
}

func TestListSkipsDotDirs(t *testing.T) {
	dir := t.TempDir()
	v, _ := Open(dir)
	if _, err := v.Write(Note{Title: "Real", Body: "x\n"}); err != nil {
		t.Fatal(err)
	}
	// A file under a dot-dir (like .dcode/index notes) must be ignored.
	mustWriteFile(t, filepath.Join(dir, ".dcode", "cache.md"), "# cache\n")

	notes, err := v.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 1 || notes[0].Title != "Real" {
		t.Fatalf("dot-dir leaked into List: %+v", notes)
	}
}

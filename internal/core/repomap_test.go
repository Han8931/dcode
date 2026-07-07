package core

import (
	"strings"
	"testing"

	"dcode/internal/vault"
)

func TestGoDefsCapturesSignatures(t *testing.T) {
	defs, err := goDefs("package p\n\n" +
		"type Service struct{ n int }\n\n" +
		"// New builds a Service.\n" +
		"func New(n int) *Service { return &Service{n} }\n\n" +
		"func (s *Service) Do(a,\n\tb string) (int, error) { return 0, nil }\n\n" +
		"const c = 1\n") // const is intentionally not a structural def
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range defs {
		got[d.name] = d.sig
	}
	if got["Service"] != "type Service struct" {
		t.Errorf("Service sig = %q", got["Service"])
	}
	if got["New"] != "func New(n int) *Service" {
		t.Errorf("New sig = %q", got["New"])
	}
	// A wrapped multi-line signature collapses to one line, receiver included.
	if got["Do"] != "func (s *Service) Do(a, b string) (int, error)" {
		t.Errorf("Do sig = %q", got["Do"])
	}
	if _, ok := got["c"]; ok {
		t.Error("const should not be captured as a structural definition")
	}
}

func TestBuildRepoMapRanksByReferences(t *testing.T) {
	files := []vault.Note{
		{RelPath: "core.go", Body: "package p\nfunc Popular() {}\nfunc Quiet() {}\n"},
		{RelPath: "a.go", Body: "package p\nfunc a() { Popular() }\n"},
		{RelPath: "b.go", Body: "package p\nfunc b() { Popular() }\n"},
		{RelPath: "readme.md", Body: "# not source, Popular Quiet\n"},
	}
	m := buildRepoMap(files)
	if len(m) == 0 || m[0].path != "core.go" {
		t.Fatalf("most-referenced file should rank first, got %+v", m)
	}
	// Within core.go, the referenced symbol outranks the unreferenced one.
	if m[0].defs[0].name != "Popular" {
		t.Fatalf("Popular should outrank Quiet, got %+v", m[0].defs)
	}
	// Markdown contributes no definitions to the map.
	for _, f := range m {
		if f.path == "readme.md" {
			t.Fatal("markdown should not appear in the repo map")
		}
	}
}

func TestRenderRepoMapFocusAndBudget(t *testing.T) {
	m := []mapFile{
		{path: "a.go", defs: []symDef{{sig: "func A()"}}},
		{path: "b.go", defs: []symDef{{sig: "func B()"}}},
	}
	// A focus filter renders only the requested paths.
	out := renderRepoMap(m, map[string]bool{"a.go": true}, 1000)
	if !strings.Contains(out, "a.go") || strings.Contains(out, "b.go") {
		t.Fatalf("focus filter should render only a.go:\n%s", out)
	}
	// A tiny budget truncates rather than dumping everything.
	out = renderRepoMap(m, nil, 5)
	if !strings.Contains(out, "truncated") {
		t.Fatalf("small budget should mark truncation:\n%s", out)
	}
}

func TestChangedPathsAndFocus(t *testing.T) {
	diff := "diff --git a/core.go b/core.go\n" +
		"index 111..222 100644\n--- a/core.go\n+++ b/core.go\n" +
		"@@ -1 +1 @@\n-func Popular() {}\n+func Popular() int { return 1 }\n"
	changed := changedPaths(diff)
	if !changed["core.go"] || len(changed) != 1 {
		t.Fatalf("changedPaths = %v, want {core.go}", changed)
	}
	files := []vault.Note{
		{RelPath: "core.go", Body: "package p\nfunc Popular() int { return 1 }\n"},
		{RelPath: "caller.go", Body: "package p\nfunc c() { Popular() }\n"},
		{RelPath: "stranger.go", Body: "package p\nfunc s() {}\n"},
	}
	focus := focusFiles(buildRepoMap(files), files, changed)
	// The changed file and the file that calls into it are in focus; the
	// unrelated file is not.
	if !focus["core.go"] || !focus["caller.go"] {
		t.Fatalf("focus should include the changed file and its caller: %v", focus)
	}
	if focus["stranger.go"] {
		t.Fatalf("focus should exclude unrelated files: %v", focus)
	}
}

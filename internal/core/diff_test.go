package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiffRange(t *testing.T) {
	cases := map[string][]string{
		"":             {"HEAD"},
		"  ":           {"HEAD"},
		"main":         {"main"},
		"HEAD~1":       {"HEAD~1"},
		"a.go b.go":    {"a.go", "b.go"},
		"  main  dev ": {"main", "dev"},
	}
	for in, want := range cases {
		if got := DiffRange(in); !reflect.DeepEqual(got, want) {
			t.Errorf("DiffRange(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSaveDiffExplanationWritesUnderDiffs(t *testing.T) {
	s, _, _ := explainService(t, map[string]string{"main.go": "package main\n"})
	meta, err := s.SaveDiffExplanation("HEAD~1", "Renamed a function.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(meta.Path, "diffs/") || !strings.HasSuffix(meta.Path, ".md") {
		t.Fatalf("diff note path = %q, want diffs/<slug>.md", meta.Path)
	}
	n, err := s.OpenNote(meta.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Body, "Change Explanation") || !strings.Contains(n.Body, "Renamed a function.") {
		t.Fatalf("diff note body wrong:\n%s", n.Body)
	}
	// Empty rev gets a stable working-changes name.
	meta2, err := s.SaveDiffExplanation("", "x")
	if err != nil {
		t.Fatal(err)
	}
	if meta2.Path != "diffs/working-changes.md" {
		t.Fatalf("empty-rev diff note path = %q, want diffs/working-changes.md", meta2.Path)
	}
}

func TestGitDiffReadsWorkingChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	s, codeDir, _ := explainService(t, map[string]string{"a.go": "package main\n\nfunc main() {}\n"})

	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", codeDir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@e.st")
	run("config", "user.name", "t")
	run("add", ".")
	run("commit", "-m", "init")

	// Change the committed file.
	if err := os.WriteFile(filepath.Join(codeDir, "a.go"), []byte("package main\n\nfunc main() { println(\"hi\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff, err := s.gitDiff(context.Background(), DiffRange(""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "a.go") || !strings.Contains(diff, "println") {
		t.Fatalf("working-tree diff should show the change:\n%s", diff)
	}

	// A bad revision surfaces git's error rather than panicking.
	if _, err := s.gitDiff(context.Background(), []string{"definitely-not-a-rev"}); err == nil {
		t.Fatal("expected an error for a bad revision")
	}
}

package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectTestCommand(t *testing.T) {
	s, codeDir, _ := explainService(t, map[string]string{"main.go": "package main\n"})
	// No project markers yet: nothing to detect.
	if cmd, ok := s.DetectTestCommand(); ok {
		t.Fatalf("empty project should detect nothing, got %q", cmd)
	}
	if err := os.WriteFile(filepath.Join(codeDir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cmd, ok := s.DetectTestCommand(); !ok || cmd != "go test ./..." {
		t.Fatalf("go.mod should detect `go test ./...`, got %q ok=%v", cmd, ok)
	}
}

func TestVerifyStreamPassAndFail(t *testing.T) {
	s, _, _ := explainService(t, map[string]string{"main.go": "package main\n"})

	// A passing command: output is streamed and the verdict is PASSED.
	var got strings.Builder
	out, err := s.VerifyStream(context.Background(), "echo hello-from-verify", func(d string) { got.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.String(), "hello-from-verify") {
		t.Fatalf("run output should stream to onDelta:\n%s", got.String())
	}
	if !strings.Contains(out, "✓ PASSED") {
		t.Fatalf("verdict should be PASSED, got %q", out)
	}

	// A failing command: not an error — the verdict is FAILED, and offline the
	// interpretation is skipped with a note instead of a model call.
	got.Reset()
	out, err = s.VerifyStream(context.Background(), "echo boom && exit 3", func(d string) { got.WriteString(d) })
	if err != nil {
		t.Fatalf("a failing run is a verdict, not an error: %v", err)
	}
	if !strings.Contains(out, "✗ FAILED") || !strings.Contains(out, "offline") {
		t.Fatalf("offline failure should carry the FAILED verdict + offline note, got %q", out)
	}
	if !strings.Contains(got.String(), "boom") {
		t.Fatal("failing run's output should still stream")
	}

	// An empty command errors up front.
	if _, err := s.VerifyStream(context.Background(), "  ", nil); err == nil {
		t.Fatal("empty command should error")
	}
}

func TestParseTestFilesAndStaging(t *testing.T) {
	text := "Finding 1 is testable.\n\n" +
		"File: pkg/foo_test.go\n```go\npackage pkg\n\nfunc TestFoo(t *testing.T) {}\n```\n\n" +
		"**File:** `sub/dir/bar_test.py`\n```python\ndef test_bar():\n    assert False\n```\n\n" +
		"File: ../evil.go\n```go\npackage evil\n```\n" + // traversal: dropped
		"File: /abs/evil.go\n```go\npackage evil\n```\n" // absolute: dropped
	files := parseTestFiles(text)
	if len(files) != 2 {
		t.Fatalf("want 2 safe files, got %d: %+v", len(files), files)
	}
	if files[0].Path != "pkg/foo_test.go" || !strings.Contains(files[0].Body, "TestFoo") {
		t.Fatalf("plain header parse wrong: %+v", files[0])
	}
	if files[1].Path != "sub/dir/bar_test.py" || !strings.Contains(files[1].Body, "test_bar") {
		t.Fatalf("bold/backtick header parse wrong: %+v", files[1])
	}

	// SaveGeneratedTests stages them as real files under the notes root.
	s, _, notesDir := explainService(t, map[string]string{"main.go": "package main\n"})
	meta, err := s.SaveGeneratedTests("main", text)
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(notesDir, "reproduction", "main", "pkg", "foo_test.go")
	body, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("staged file missing: %v", err)
	}
	if !strings.Contains(string(body), "TestFoo") {
		t.Fatalf("staged file contents wrong:\n%s", body)
	}
	// The escaping paths were never written anywhere.
	if _, err := os.Stat(filepath.Join(notesDir, "evil.go")); err == nil {
		t.Fatal("path traversal escaped the staging dir")
	}
	// The note documents the staging dir and the copy step.
	n, err := s.OpenNote(meta.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Body, "reproduction") || !strings.Contains(n.Body, "cp -R") {
		t.Fatalf("tests note should document staging + copy:\n%s", n.Body)
	}
}

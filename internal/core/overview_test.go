package core

import (
	"strings"
	"testing"
)

func TestProjectDigestListsFilesAndKeyHeads(t *testing.T) {
	s, _, _ := explainService(t, map[string]string{
		"main.go":            "package main\n\nfunc main() { run() }\n",
		"internal/app/run.go": "package app\n\nfunc run() {}\n",
		"notes.md":           "# not source\n",
	})
	d := s.projectDigest()

	if !strings.Contains(d, "Source files:") || !strings.Contains(d, "main.go") ||
		!strings.Contains(d, "internal/app/run.go") {
		t.Fatalf("digest should list all source paths:\n%s", d)
	}
	// The shallowest file (main.go) is a key file whose head is inlined.
	if !strings.Contains(d, "--- main.go ---") || !strings.Contains(d, "func main") {
		t.Fatalf("shallow entry-point file should be inlined as a key file:\n%s", d)
	}
	// Markdown is never source.
	if strings.Contains(d, "notes.md") {
		t.Fatalf("markdown should not appear in the project digest:\n%s", d)
	}
}

func TestSaveOverviewWritesTopLevelNote(t *testing.T) {
	s, _, _ := explainService(t, map[string]string{"main.go": "package main\n"})
	meta, err := s.SaveOverview("It is a CLI.")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Path != "OVERVIEW.md" {
		t.Fatalf("overview path = %q, want OVERVIEW.md", meta.Path)
	}
	n, err := s.OpenNote("OVERVIEW.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Body, "Architecture Overview") || !strings.Contains(n.Body, "It is a CLI.") {
		t.Fatalf("overview note body wrong:\n%s", n.Body)
	}
}

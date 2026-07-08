package core

import (
	"context"
	"strings"
	"testing"
)

func TestSaveReviewWritesUnderReviews(t *testing.T) {
	s, _, _ := explainService(t, map[string]string{"main.go": "package main\n"})
	meta, err := s.SaveReview("main", "1. [high] main.go:3 — off-by-one.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(meta.Path, "reviews/") || !strings.HasSuffix(meta.Path, ".md") {
		t.Fatalf("review note path = %q, want reviews/<slug>.md", meta.Path)
	}
	n, err := s.OpenNote(meta.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.Body, "Code Review") || !strings.Contains(n.Body, "off-by-one") {
		t.Fatalf("review note body wrong:\n%s", n.Body)
	}
	// Empty rev gets the stable working-changes name, like :diff.
	meta2, err := s.SaveReview("", "x")
	if err != nil {
		t.Fatal(err)
	}
	if meta2.Path != "reviews/working-changes.md" {
		t.Fatalf("empty-rev review path = %q, want reviews/working-changes.md", meta2.Path)
	}
}

func TestReviewStreamErrorsOnEmptyDiff(t *testing.T) {
	// A non-repo project: git diff fails, and ReviewStream surfaces it rather
	// than reviewing nothing.
	s, _, _ := explainService(t, map[string]string{"a.go": "package main\n"})
	_, err := s.ReviewStream(context.Background(), "", nil)
	if err == nil {
		t.Fatal("ReviewStream outside a git repo should error, not review nothing")
	}
}

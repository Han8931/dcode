package tui

import (
	"strings"
	"testing"

	"dcode/internal/config"
	"dcode/internal/core"
	"dcode/internal/tutor"
	"dcode/internal/vault"
)

// :review needs an AI provider; offline it guards instead of streaming.
func TestVaultReviewOfflineGuard(t *testing.T) {
	m := newTestVaultModel(t)
	tm, cmd := m.runEx("review")
	m = tm.(VaultModel)
	if m.streaming || m.reviewMode {
		t.Fatal("offline :review must not start a stream")
	}
	if cmd != nil || !strings.Contains(m.notice, "AI provider") {
		t.Fatalf("offline :review should flash a provider hint, got %q", m.notice)
	}
}

// :tests requires a completed :review to work from (and offline it guards on
// the provider first, like every AI command).
func TestVaultTestsGuards(t *testing.T) {
	// Offline: provider guard.
	m := newTestVaultModel(t)
	tm, cmd := m.runEx("tests")
	m = tm.(VaultModel)
	if cmd != nil || !strings.Contains(m.notice, "AI provider") {
		t.Fatalf("offline :tests should flash a provider hint, got %q", m.notice)
	}

	// Online (a local provider needs no key) but no review yet: the review guard.
	code, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := core.New(code, notes, tutor.New(config.AIConfig{Provider: "ollama"}))
	if svc.Offline() {
		t.Fatal("ollama provider should count as online (no key required)")
	}
	m2 := newVaultModel(svc, config.Config{})
	tm, cmd = m2.runEx("tests")
	m2 = tm.(VaultModel)
	if cmd != nil || m2.streaming {
		t.Fatal(":tests without a review must not start a stream")
	}
	if !strings.Contains(m2.notice, ":review first") {
		t.Fatalf(":tests without a review should point at :review, got %q", m2.notice)
	}
}

// A completed :tests stream saves the generated files beside the review.
func TestVaultTestsCompletionSaves(t *testing.T) {
	m := newTestVaultModel(t)
	m.streaming, m.testsMode, m.pending = true, true, 1
	m.lastReviewRev = "main"
	m.chat.beginStream()

	tm, cmd := m.handleStreamChunk(streamChunkMsg{done: true, full: "File: a_test.go\n```go\nfunc TestX(t *testing.T) {}\n```"})
	m = tm.(VaultModel)
	if m.streaming || m.testsMode {
		t.Fatal("completion should clear streaming/testsMode")
	}
	if cmd == nil {
		t.Fatal("completed :tests should dispatch the save command")
	}
	opened, ok := cmd().(vOpenedMsg)
	if !ok {
		t.Fatalf("save should open the tests note, got %T", cmd())
	}
	if opened.note.Path != "reviews/main-tests.md" {
		t.Fatalf("tests note path = %q, want reviews/main-tests.md", opened.note.Path)
	}
	// The parseable file block was staged as a real file, and the note explains
	// the copy-and-run step.
	if !strings.Contains(opened.note.Body, "Staged as real files") ||
		!strings.Contains(opened.note.Body, "cp -R") ||
		!strings.Contains(opened.note.Body, "TestX") {
		t.Fatalf("tests note body wrong:\n%s", opened.note.Body)
	}
}

// When a review stream completes, the findings auto-save under reviews/.
func TestVaultReviewCompletionSaves(t *testing.T) {
	m := newTestVaultModel(t)
	m.streaming, m.reviewMode, m.pending = true, true, 1
	m.reviewRev = "main"
	m.chat.beginStream()

	tm, cmd := m.handleStreamChunk(streamChunkMsg{done: true, full: "1. [low] fine."})
	m = tm.(VaultModel)
	if m.streaming || m.reviewMode {
		t.Fatal("completion should clear streaming/reviewMode")
	}
	// The findings are retained so :tests can turn them into reproduction tests.
	if m.lastReview != "1. [low] fine." || m.lastReviewRev != "main" {
		t.Fatalf("completed review should be retained for :tests, got %q (%q)", m.lastReview, m.lastReviewRev)
	}
	if cmd == nil {
		t.Fatal("completed review should dispatch the save command")
	}
	opened, ok := cmd().(vOpenedMsg)
	if !ok {
		t.Fatalf("save should open the review note, got %T", cmd())
	}
	if !strings.HasPrefix(opened.note.Path, "reviews/") ||
		!strings.Contains(opened.note.Body, "[low] fine.") {
		t.Fatalf("review note wrong: %q\n%s", opened.note.Path, opened.note.Body)
	}
}

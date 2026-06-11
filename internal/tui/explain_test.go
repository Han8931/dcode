package tui

import (
	"strings"
	"testing"

	"dcode/internal/core"
)

// Opening a source file (Source:"code") marks the buffer read-only and suspends
// autosave, so the user's source is never rewritten.
func TestVaultOpenSourceIsReadOnly(t *testing.T) {
	m := newTestVaultModel(t)
	tm, _ := m.Update(vOpenedMsg{note: core.Note{
		NoteMeta: core.NoteMeta{Path: "pkg/foo.go", Title: "foo.go"},
		Body:     "package pkg\n\nfunc Foo() {}\n",
		Source:   "code",
	}})
	m = tm.(VaultModel)
	if !m.readOnly {
		t.Fatal("source file should open read-only")
	}
	if *m.curPath != "" {
		t.Fatal("autosave must be suspended for read-only source (curPath blank)")
	}
	// A markdown note, by contrast, is editable.
	tm, _ = m.Update(vOpenedMsg{note: core.Note{
		NoteMeta: core.NoteMeta{Path: "n.md", Title: "n"}, Body: "# n\n", Source: "user",
	}})
	m = tm.(VaultModel)
	if m.readOnly || *m.curPath != "n.md" {
		t.Fatal("markdown notes should be editable with autosave on")
	}
}

// :explain needs an AI provider; offline it guards instead of streaming.
func TestVaultExplainOfflineGuard(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "n.md", "# n\n")
	tm, cmd := m.runEx("explain")
	m = tm.(VaultModel)
	if m.streaming || m.explaining {
		t.Fatal("offline :explain must not start a stream")
	}
	if cmd != nil || !strings.Contains(m.notice, "AI provider") {
		t.Fatalf("offline :explain should flash a provider hint, got %q", m.notice)
	}
}

// When an explanation stream completes, the text is parked in lastExplain, the
// conversation is seeded for follow-ups, and :note saves it as a companion note.
func TestVaultExplainCompletionAndSave(t *testing.T) {
	m := newTestVaultModel(t)
	// Simulate a source file open and an in-flight explanation.
	m.current, m.currentTitle, m.readOnly = "pkg/foo.go", "foo.go", true
	m.streaming, m.explaining, m.pending = true, true, 1
	m.lastExplainPath = m.current
	m.chat.beginStream()

	tm, _ := m.handleStreamChunk(streamChunkMsg{done: true, full: "Foo prints nothing."})
	m = tm.(VaultModel)
	if m.streaming || m.explaining {
		t.Fatal("completion should clear streaming/explaining")
	}
	if m.lastExplain != "Foo prints nothing." {
		t.Fatalf("lastExplain = %q", m.lastExplain)
	}
	if len(m.chatHist) != 2 || m.chatHist[1].Role != "assistant" {
		t.Fatalf("conversation should be seeded for follow-ups: %+v", m.chatHist)
	}

	// :note saves the explanation as a companion note mirroring the source path.
	tm, cmd := m.runEx("note")
	m = tm.(VaultModel)
	if cmd == nil {
		t.Fatal(":note should issue a save command")
	}
	saved, ok := cmd().(vExplanationSavedMsg)
	if !ok {
		t.Fatalf("expected vExplanationSavedMsg, got %T", cmd())
	}
	if saved.meta.Path != "pkg/foo.go.md" {
		t.Fatalf("companion note path = %q, want pkg/foo.go.md", saved.meta.Path)
	}
}

// :note with nothing explained yet is a guarded no-op.
func TestVaultNoteWithoutExplanation(t *testing.T) {
	m := newTestVaultModel(t)
	_, cmd := m.runEx("note")
	if cmd != nil {
		t.Fatal(":note before any :explain should be a no-op")
	}
}

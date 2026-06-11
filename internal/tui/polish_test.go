package tui

import (
	"strings"
	"testing"

	"dcode/internal/core"
	"dcode/internal/editor"
	"dcode/internal/tutor"
)

// openNote saves and opens a note in the model, returning the updated model.
func openNote(t *testing.T, m VaultModel, path, body string) VaultModel {
	t.Helper()
	opened := vSaveOpenCmd(m.svc, path, body)().(vOpenedMsg)
	tm, _ := m.Update(opened)
	return tm.(VaultModel)
}

// A completed polish stream parks the rewrite in pendingEdit; :apply writes it
// into the editor and saves; the original note is untouched until then.
func TestVaultPolishApplyFlow(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "Notes.md", "# notes\n\nrough draft\n")

	// Simulate a polish stream in flight, then its completion (no network).
	m.streaming, m.polishing, m.pending = true, true, 1
	m.pendingEditPath = m.current
	m.chat.beginStream()
	tm, _ := m.handleStreamChunk(streamChunkMsg{done: true, full: "# Notes\n\nPolished draft.\n"})
	m = tm.(VaultModel)

	if m.streaming || m.polishing {
		t.Fatal("stream completion should clear streaming/polishing")
	}
	if m.pendingEdit != "# Notes\n\nPolished draft.\n" {
		t.Fatalf("pendingEdit = %q", m.pendingEdit)
	}
	// The note is NOT changed yet — review-then-apply.
	if got := m.editor.Value(); !strings.Contains(got, "rough draft") {
		t.Fatalf("editor changed before :apply: %q", got)
	}

	// :apply swaps it in and issues a save.
	tm, cmd := m.runEx("apply")
	m = tm.(VaultModel)
	if m.pendingEdit != "" {
		t.Fatal(":apply should clear the pending edit")
	}
	if got := m.editor.Value(); !strings.Contains(got, "Polished draft.") {
		t.Fatalf("editor not updated by :apply: %q", got)
	}
	if cmd == nil {
		t.Fatal(":apply should save the note")
	}
}

// A selection-scoped proposal applies back to just its span (verified
// unchanged), leaving the rest of the note alone.
func TestVaultSelectionEditApply(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "N.md", "x")
	m.editor.SetValue("alpha beta gamma") // exact buffer for stable rune indices

	m.pendingEdit = "BRAVO"
	m.pendingEditPath = m.current
	m.pendingSel = &editor.Selection{Text: "beta", Start: 6, Cut: 10}

	tm, cmd := m.runEx("apply")
	m = tm.(VaultModel)
	if got := m.editor.Value(); got != "alpha BRAVO gamma" {
		t.Fatalf("selection apply: %q", got)
	}
	if m.pendingEdit != "" || m.pendingSel != nil {
		t.Fatal(":apply should clear the pending state")
	}
	if cmd == nil {
		t.Fatal(":apply should save the note")
	}
}

// If the span changed since the proposal, :apply refuses rather than clobber.
func TestVaultSelectionEditStaleRefused(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "N.md", "x")
	m.editor.SetValue("alpha beta gamma")

	m.pendingEdit = "BRAVO"
	m.pendingEditPath = m.current
	m.pendingSel = &editor.Selection{Text: "DELTA", Start: 6, Cut: 10} // want mismatch

	tm, _ := m.runEx("apply")
	m = tm.(VaultModel)
	if got := m.editor.Value(); got != "alpha beta gamma" {
		t.Fatalf("a changed span must not be applied: %q", got)
	}
	if m.pendingEdit != "" || m.pendingSel != nil {
		t.Fatal("a refused apply should still clear the stale proposal")
	}
	if !strings.Contains(m.notice, "changed under the selection") {
		t.Fatalf("notice = %q", m.notice)
	}
}

// :ask on a selection grounds the chat on that excerpt and sends the question.
func TestVaultAskSelectionGroundsAndSends(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "N.md", "# n\n\nThe argument is weak here.\n")
	m.cmdSel = &editor.Selection{Text: "The argument is weak here."}

	tm, cmd := m.cmdAsk("why is it weak?")
	m = tm.(VaultModel)
	if m.focusExcerpt != "The argument is weak here." {
		t.Fatalf("excerpt not set: %q", m.focusExcerpt)
	}
	if !m.streaming || cmd == nil {
		t.Fatal(":ask with a question should start a reply")
	}
	// The excerpt now grounds the conversation context.
	ctx := m.chatContext()
	if !strings.Contains(ctx, "weak here") || !strings.Contains(ctx, "SELECTED") {
		t.Fatalf("context missing the excerpt:\n%s", ctx)
	}
}

// :ask on a selection with no question parks the excerpt and focuses the chat.
func TestVaultAskSelectionNoQuestionFocusesChat(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "N.md", "# n\n\nbody\n")
	m.cmdSel = &editor.Selection{Text: "body"}

	tm, _ := m.cmdAsk("")
	m = tm.(VaultModel)
	if m.focusExcerpt != "body" {
		t.Fatal("excerpt should be set even without a question")
	}
	if m.streaming {
		t.Fatal("no question yet — should not stream")
	}
	if m.focus != paneChat {
		t.Fatal(":ask should focus the chat so the learner can type")
	}
}

// Regression: on a long note the focused excerpt must survive context
// clamping (it leads the context, so the note body — not the excerpt — is what
// gets trimmed). Previously the excerpt was appended last and truncated away,
// so the tutor lost the discussion's subject.
func TestVaultAskExcerptSurvivesClampingLongNote(t *testing.T) {
	m := newTestVaultModel(t)
	long := "# Big\n\n" + strings.Repeat("filler sentence about something. ", 600) // ~19k chars
	m = openNote(t, m, "Big.md", long)
	m.cmdSel = &editor.Selection{Text: "THE-KEY-EXCERPT-XYZ"}

	tm, _ := m.cmdAsk("")
	m = tm.(VaultModel)

	clamped := core.ClampContext(m.chatContext())
	if !strings.Contains(clamped, "THE-KEY-EXCERPT-XYZ") {
		t.Fatal("the focused excerpt must survive clamping on a long note")
	}
	if !strings.HasPrefix(strings.TrimSpace(clamped), "The learner has SELECTED") {
		t.Fatalf("context should lead with the excerpt, got:\n%.120s", clamped)
	}
}

// The excerpt is pinned to the LATEST user turn every request, so a follow-up
// question stays grounded on the selection even after the model asked for more
// detail (the bug: the selection lived only in far-off system context, which
// small models drift away from after a turn or two).
func TestGroundHistoryPinsExcerptToLastUserTurn(t *testing.T) {
	hist := []tutor.ChatTurn{
		{Role: "user", Content: "explain this code"},
		{Role: "assistant", Content: "Could you be more specific?"},
		{Role: "user", Content: "explain about the code again"},
	}
	out := groundHistory(hist, "for i in range(n): total += i")

	last := out[len(out)-1].Content
	if !strings.Contains(last, "for i in range(n)") {
		t.Fatalf("follow-up turn should carry the excerpt:\n%s", last)
	}
	if !strings.Contains(last, "explain about the code again") {
		t.Fatalf("follow-up turn should keep the question:\n%s", last)
	}
	// Only the latest user turn is augmented; earlier turns and the input are
	// left untouched (the transcript stays clean).
	if strings.Contains(out[0].Content, "range(n)") {
		t.Fatal("earlier turns must not be rewritten")
	}
	if hist[len(hist)-1].Content != "explain about the code again" {
		t.Fatal("groundHistory must not mutate the input history")
	}

	// No excerpt, or a non-user last turn, changes nothing.
	if got := groundHistory(hist, ""); got[len(got)-1].Content != "explain about the code again" {
		t.Fatal("no excerpt should leave history unchanged")
	}
	asst := []tutor.ChatTurn{{Role: "assistant", Content: "hi"}}
	if got := groundHistory(asst, "x"); got[0].Content != "hi" {
		t.Fatal("a non-user last turn should be left alone")
	}
}

// Opening another note ends the discussion of the previous note's excerpt.
func TestVaultAskClearedOnNoteSwitch(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "A.md", "# a\n\nthing\n")
	m.cmdSel = &editor.Selection{Text: "thing"}
	tm, _ := m.cmdAsk("")
	m = tm.(VaultModel)
	if m.focusExcerpt == "" {
		t.Fatal("excerpt should be set")
	}
	m = openNote(t, m, "B.md", "# b\n")
	if m.focusExcerpt != "" {
		t.Fatal("opening another note should end the discussion")
	}
}

// Bare :ask with no selection and no question is a guided no-op.
func TestVaultAskNeedsSelectionOrQuestion(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "N.md", "# n\n")
	tm, _ := m.runEx("ask")
	m = tm.(VaultModel)
	if m.streaming || m.focusExcerpt != "" {
		t.Fatal("bare :ask should no-op")
	}
	if !strings.Contains(m.notice, "select text") {
		t.Fatalf("notice = %q", m.notice)
	}
}

// :discard drops the proposal without touching the note.
func TestVaultPolishDiscard(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "N.md", "# n\n\nbody\n")
	before := m.editor.Value()
	m.pendingEdit, m.pendingEditPath = "REWRITTEN", m.current

	tm, _ := m.runEx("discard")
	m = tm.(VaultModel)
	if m.pendingEdit != "" {
		t.Fatal(":discard should clear the pending edit")
	}
	if m.editor.Value() != before {
		t.Fatal(":discard must not change the note")
	}
}

// Switching notes drops a proposal made for the previous note.
func TestVaultPolishClearedOnNoteSwitch(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "A.md", "# a\n")
	m.pendingEdit, m.pendingEditPath = "for A", m.current

	m = openNote(t, m, "B.md", "# b\n")
	if m.pendingEdit != "" {
		t.Fatal("opening another note should drop the pending edit")
	}
}

// Offline, :polish refuses with guidance instead of streaming junk.
func TestVaultPolishOfflineRefused(t *testing.T) {
	m := newTestVaultModel(t) // newTestVaultModel uses an offline tutor
	m = openNote(t, m, "N.md", "# n\n")

	tm, _ := m.runEx("polish")
	m = tm.(VaultModel)
	if m.streaming || m.polishing {
		t.Fatal("offline :polish must not start a stream")
	}
	if !strings.Contains(m.notice, "AI provider") {
		t.Fatalf("offline notice = %q", m.notice)
	}
}

// :polish with no note open, and :apply with nothing pending, both no-op.
func TestVaultPolishNoNoteAndEmptyApply(t *testing.T) {
	m := newTestVaultModel(t)
	tm, _ := m.runEx("polish")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "open a note first") {
		t.Fatalf("no-note :polish notice = %q", m.notice)
	}
	tm, _ = m.runEx("apply")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "nothing to apply") {
		t.Fatalf("empty :apply notice = %q", m.notice)
	}
}

// :edit requires an instruction.
func TestVaultEditRequiresInstruction(t *testing.T) {
	m := newTestVaultModel(t)
	m = openNote(t, m, "N.md", "# n\n")
	tm, _ := m.runEx("edit")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "usage: :edit") {
		t.Fatalf("bare :edit notice = %q", m.notice)
	}
	if m.streaming {
		t.Fatal("bare :edit must not start a stream")
	}
}

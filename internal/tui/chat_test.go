package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"dcode/internal/editor"
)

// forceColorTUI makes lipgloss emit ANSI in tests so style assertions work.
func forceColorTUI(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// The app must pin ambiguous-width characters to one cell so every pane (chat
// viewport via uniseg, editor soft-wrap via go-runewidth) measures the arrows,
// ≤/≥, · and ² that fill AI replies. Under a CJK locale go-runewidth
// would otherwise call them 2 cells and the layout would drift.
func TestNormalizeRuneWidthNarrowsAmbiguous(t *testing.T) {
	prev := runewidth.DefaultCondition.EastAsianWidth
	runewidth.DefaultCondition.EastAsianWidth = true // simulate a CJK locale
	t.Cleanup(func() { runewidth.DefaultCondition.EastAsianWidth = prev })

	normalizeRuneWidth()

	for _, r := range []rune{'→', '≤', '≥', '·', '×', '²', '—'} {
		if w := runewidth.RuneWidth(r); w != 1 {
			t.Errorf("RuneWidth(%q) = %d after normalizeRuneWidth, want 1", r, w)
		}
	}
}

func TestChatBusyLineShowsProgress(t *testing.T) {
	c := newChat()
	c.setSize(40, 12)
	c.setBusy("tutor thinking")
	c.tickBusy()
	if !strings.Contains(c.view(), "tutor thinking…") {
		t.Fatalf("busy line missing from view:\n%s", c.view())
	}
	// The pane must not grow: busy steals a transcript row instead.
	if got := strings.Count(c.view(), "\n") + 1; got > 12 {
		t.Fatalf("view is %d rows, want <= 12", got)
	}
	c.setBusy("")
	if strings.Contains(c.view(), "thinking") {
		t.Fatal("busy line should disappear when idle")
	}
}

func TestChatInputIsMultiRowAndSubmits(t *testing.T) {
	c := newChat()
	c.setSize(30, 12)
	c.input.SetValue("a rather long question that wraps over the narrow pane width")
	got, ok := c.submit()
	if !ok || !strings.HasPrefix(got, "a rather long") {
		t.Fatalf("submit = %q ok=%v", got, ok)
	}
	if v := c.input.Value(); v != "" {
		t.Fatalf("input should clear after submit, got %q", v)
	}
	// Three input rows are reserved (height permitting).
	if c.input.Height() != chatInputRows {
		t.Fatalf("input height = %d, want %d", c.input.Height(), chatInputRows)
	}
}

func TestChatSpeakerBadges(t *testing.T) {
	forceColorTUI(t)
	c := newChat()
	c.setSize(40, 12)
	c.append(roleUser, "question")
	c.append(roleTutor, "reply")
	content := c.renderBlock(chatBlock{role: roleTutor, text: "reply"})
	if !strings.Contains(content, chatTutorBadge.Render(" tutor ")) {
		t.Fatalf("tutor badge missing:\n%q", content)
	}
	content = c.renderBlock(chatBlock{role: roleUser, text: "question"})
	if !strings.Contains(content, chatUserBadge.Render(" you ")) {
		t.Fatalf("you badge missing:\n%q", content)
	}
}

func TestChatHighlightsFencedCode(t *testing.T) {
	forceColorTUI(t)
	c := newChat()
	c.setSize(60, 12)
	body := "Here is an example:\n```python\ndef add(a, b):\n    return a + b\n```\nTry it."
	got := c.renderRichBody(body)
	if strings.Contains(got, "```") {
		t.Fatalf("fence markers should not be rendered:\n%q", got)
	}
	wantLine := editor.Highlight("python", "def add(a, b):")
	if !strings.Contains(got, wantLine) {
		t.Fatalf("code should be syntax-highlighted\nwant fragment %q\nin:\n%q", wantLine, got)
	}
	if !strings.Contains(got, "Try it.") {
		t.Fatalf("prose after the fence lost:\n%q", got)
	}
	if !strings.Contains(got, "\x1b[1;38;5;111m│ \x1b[0m") {
		t.Fatalf("code gutter should be colored in chat:\n%q", got)
	}
	if !strings.Contains(got, "\x1b[48;5;232m") {
		t.Fatalf("code rows should have a distinct background:\n%q", got)
	}
}

func TestEnableTUIColorForcesANSIProfile(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR", "")
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	enableTUIColor()
	if got := chatTutorBadge.Render(" tutor "); !strings.Contains(got, "\x1b[") {
		t.Fatalf("TUI color profile should emit ANSI, got %q", got)
	}
}

func TestChatHighlightsUserFencedCode(t *testing.T) {
	forceColorTUI(t)
	c := newChat()
	c.setSize(60, 12)
	got := c.renderBlock(chatBlock{role: roleUser, text: "Why fails?\n```go\nfunc main() {}\n```"})
	if !strings.Contains(got, editor.Highlight("go", "func main() {}")) {
		t.Fatalf("user code fence should be syntax-highlighted:\n%q", got)
	}
}

func TestChatFenceInfoStringUsesFirstLanguageToken(t *testing.T) {
	forceColorTUI(t)
	c := newChat()
	c.setSize(60, 12)
	got := c.renderRichBody("```go title=main.go\nfunc main() {}\n```")
	if !strings.Contains(got, editor.Highlight("go", "func main() {}")) {
		t.Fatalf("fence info string should use first language token:\n%q", got)
	}
	got = c.renderRichBody("```{.python}\ndef add(a, b):\n    return a + b\n```")
	if !strings.Contains(got, editor.Highlight("python", "def add(a, b):")) {
		t.Fatalf("braced class language should be recognized:\n%q", got)
	}
}

func TestChatHighlightsTildeFences(t *testing.T) {
	forceColorTUI(t)
	c := newChat()
	c.setSize(60, 12)
	got := c.renderRichBody("~~~python\ndef add(a, b):\n    return a + b\n~~~")
	if strings.Contains(got, "~~~") {
		t.Fatalf("tilde fence markers should not be rendered:\n%q", got)
	}
	if !strings.Contains(got, editor.Highlight("python", "def add(a, b):")) {
		t.Fatalf("tilde fence should be syntax-highlighted:\n%q", got)
	}
}

func TestChatCodeBlocksWrapInsteadOfClip(t *testing.T) {
	c := newChat()
	c.setSize(24, 12)
	long := "```\nresult = compute_something_quite_long(alpha, beta) # TRAILING_MARKER\n```"
	got := c.renderRichBody(long)
	// Content must survive in full — wrapped across rows, never clipped. Strip
	// the wrapping (gutters + newlines) and look for the tail marker.
	joined := strings.NewReplacer("\n", "", "│ ", "").Replace(got)
	if !strings.Contains(joined, "TRAILING_MARKER") {
		t.Fatalf("the end of a long code line must stay visible (wrapped, not clipped):\n%q", got)
	}
	// Every visual row carries the gutter, including wrapped continuations.
	for i, row := range strings.Split(got, "\n") {
		if !strings.Contains(row, "│") {
			t.Fatalf("row %d lost the code gutter: %q", i, row)
		}
	}
}

func TestChatDragSelectExtractsText(t *testing.T) {
	forceColorTUI(t)
	c := newChat()
	c.setSize(40, 12)
	c.append(roleTutor, "alpha beta gamma\nsecond line here")
	// contentLines: 0 = " tutor " badge, 1-2 = the body lines.

	// Sweep from the start of body line 1 to column 4 of line 2 (inclusive).
	c.startSelect(0, 1)
	if _, ok := c.selectionText(); ok {
		t.Fatal("a plain press (no motion) should not select anything")
	}
	c.dragSelect(4, 2)
	got, ok := c.selectionText()
	if !ok {
		t.Fatal("drag should produce a selection")
	}
	if got != "alpha beta gamma\nsecon" {
		t.Fatalf("selection = %q, want %q", got, "alpha beta gamma\nsecon")
	}

	// The selected span is repainted in the selection color (the highlight
	// runs through the row's padding, terminal-style).
	// selection color = palette Text on Surface1, downsampled to ANSI-256.
	view := c.overlaySelection(c.vp.View())
	if !strings.Contains(view, "\x1b[38;5;189;48;5;59malpha beta gamma") {
		t.Fatalf("selection overlay missing:\n%q", view)
	}

	// A reversed drag (bottom-up) selects the same text.
	c.startSelect(4, 2)
	c.dragSelect(0, 1)
	if got, _ := c.selectionText(); got != "alpha beta gamma\nsecon" {
		t.Fatalf("reversed selection = %q", got)
	}

	// A fresh press clears the previous selection.
	c.startSelect(0, 1)
	if _, ok := c.selectionText(); ok {
		t.Fatal("new press should clear the selection")
	}
}

func TestChatSelectionSingleLineAndClamp(t *testing.T) {
	c := newChat()
	c.setSize(40, 12)
	c.append(roleTutor, "alpha beta gamma")

	// Mid-line sweep on one row, head cell included.
	c.startSelect(6, 1)
	c.dragSelect(9, 1)
	if got, _ := c.selectionText(); got != "beta" {
		t.Fatalf("single-line selection = %q, want beta", got)
	}

	// Coordinates beyond the transcript clamp instead of panicking.
	c.startSelect(-3, -5)
	c.dragSelect(999, 999)
	if _, ok := c.selectionText(); !ok {
		t.Fatal("clamped select-all should still produce text")
	}
}

// Releasing a drag selection copies it to the clipboard with no key press —
// the reliable path on Linux terminals that swallow Alt-C as a menu mnemonic.
func TestChatDragReleaseCopies(t *testing.T) {
	got := stubClipboard(t)
	m := newTestVaultModel(t) // sized 100x40, chat laid out
	m.chat.append(roleTutor, "copy this whole line please")

	// Simulate a drag in progress over the body line (line 0 is the badge).
	m.chat.startSelect(0, 1)
	m.chat.dragSelect(20, 1)
	m.dragChat = true

	// Release on a valid (non-title/status) cell finishes the drag.
	tm, _ := m.handleMouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 90, Y: 10})
	m = tm.(VaultModel)

	if *got == "" {
		t.Fatal("releasing a drag selection should copy to the clipboard")
	}
	if !strings.Contains(*got, "copy this whole line") {
		t.Fatalf("copied text = %q, want the selected line", *got)
	}
	if m.dragChat {
		t.Fatal("release should end the drag")
	}

	// A bare click (press then release, no motion) must NOT copy.
	*got = ""
	m.chat.clearSelect()
	m.dragChat = true
	tm, _ = m.handleMouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 90, Y: 10})
	m = tm.(VaultModel)
	if *got != "" {
		t.Fatalf("a bare click should not copy, got %q", *got)
	}
}

func TestVaultCompactGrowsChat(t *testing.T) {
	m := newTestVaultModel(t)
	before := m.chatW
	for i := 0; i < 4; i++ { // step to the clamp
		tm, _ := m.runEx("compact")
		m = tm.(VaultModel)
	}
	if m.chatW <= before {
		t.Fatalf(":compact should widen the chat pane: %d -> %d", before, m.chatW)
	}
	if m.chatW <= m.editorW {
		t.Fatalf("at max compact the chat should outsize the editor (chat=%d editor=%d)", m.chatW, m.editorW)
	}
}

func TestChatUnlabeledFenceUsesCodeLang(t *testing.T) {
	forceColorTUI(t)
	c := newChat()
	c.setSize(60, 12)
	c.codeLang = "go"
	got := c.renderRichBody("```\nfunc main() {}\n```")
	if !strings.Contains(got, editor.Highlight("go", "func main() {}")) {
		t.Fatalf("unlabeled fence should highlight as codeLang:\n%q", got)
	}
	// With no codeLang, unlabeled fences stay plain.
	c.codeLang = ""
	got = c.renderRichBody("```\nfunc main() {}\n```")
	if !strings.Contains(got, "func main() {}") {
		t.Fatalf("plain fence content lost:\n%q", got)
	}
}

func TestVaultPerNoteChat(t *testing.T) {
	m := newTestVaultModel(t)

	// Open note A and have some chat activity.
	a := vSaveOpenCmd(m.svc, "x/A.md", "# A\n\nbody\n")().(vOpenedMsg)
	tm, _ := m.Update(a)
	m = tm.(VaultModel)
	m.chat.append(roleUser, "question about A")

	// Open note B: the pane starts fresh (just the topic header).
	b := vSaveOpenCmd(m.svc, "x/B.md", "# B\n\nbody\n")().(vOpenedMsg)
	tm, _ = m.Update(b)
	m = tm.(VaultModel)
	if strings.Contains(m.chat.view(), "question about A") {
		t.Fatal("note B's chat should not show note A's activity")
	}
	if !strings.Contains(m.chat.view(), "— B —") {
		t.Fatalf("fresh note chat should show its header:\n%s", m.chat.view())
	}

	// Back to A: the old transcript is restored.
	tm, _ = m.Update(vOpenCmd(m.svc, "x/A.md")().(vOpenedMsg))
	m = tm.(VaultModel)
	if !strings.Contains(m.chat.view(), "question about A") {
		t.Fatal("returning to note A should restore its chat history")
	}
}

// stubClipboard replaces the real clipboard for a test and returns a pointer
// to the last copied text.
func stubClipboard(t *testing.T) *string {
	t.Helper()
	var got string
	prev := copyToClipboard
	copyToClipboard = func(text string) error {
		got = text
		return nil
	}
	t.Cleanup(func() { copyToClipboard = prev })
	return &got
}

func TestCopyChatLastReply(t *testing.T) {
	got := stubClipboard(t)
	c := newChat()
	c.setSize(60, 12)
	c.append(roleUser, "q1")
	c.append(roleTutor, "first answer")
	c.append(roleUser, "q2")
	c.append(roleTutor, "second answer")
	c.append(roleSystem, "some notice") // must be skipped

	notice := copyChat(&c, "")
	if *got != "second answer" {
		t.Fatalf("copied %q, want the last tutor reply", *got)
	}
	if !strings.Contains(notice, "✓ copied last reply") {
		t.Fatalf("feedback notice = %q", notice)
	}
}

func TestCopyChatCode(t *testing.T) {
	got := stubClipboard(t)
	c := newChat()
	c.setSize(60, 12)
	c.append(roleTutor, "Try this:\n```python\nx = 1\n```\nthen this:\n```python\ny = 2\n```")

	copyChat(&c, "code")
	if *got != "y = 2" {
		t.Fatalf("copied %q, want the LAST fenced block", *got)
	}
}

func TestCopyChatAllAndEmpty(t *testing.T) {
	got := stubClipboard(t)
	c := newChat()
	c.setSize(60, 12)

	notice := copyChat(&c, "") // nothing yet
	if *got != "" {
		t.Fatalf("nothing should be copied from an empty chat, got %q", *got)
	}
	if !strings.Contains(notice, "nothing to copy") {
		t.Fatalf("empty-chat notice = %q", notice)
	}

	c.append(roleUser, "hello")
	c.append(roleTutor, "hi there")
	copyChat(&c, "all")
	if !strings.Contains(*got, "you: hello") || !strings.Contains(*got, "tutor: hi there") {
		t.Fatalf("transcript copy wrong: %q", *got)
	}
}

func TestPasteIntoChatInput(t *testing.T) {
	prev := pasteFromClipboard
	pasteFromClipboard = func() (string, error) { return "pasted question", nil }
	t.Cleanup(func() { pasteFromClipboard = prev })

	c := newChat()
	c.setSize(60, 12)
	if notice := pasteChat(&c); notice != "" {
		t.Fatalf("successful paste should be silent, got %q", notice)
	}
	if got := c.input.Value(); got != "pasted question" {
		t.Fatalf("input after paste = %q", got)
	}

	// Empty clipboard: friendly notice, input untouched.
	pasteFromClipboard = func() (string, error) { return "  ", nil }
	c2 := newChat()
	c2.setSize(60, 12)
	notice := pasteChat(&c2)
	if c2.input.Value() != "" {
		t.Fatalf("empty clipboard must not modify the input")
	}
	if !strings.Contains(notice, "clipboard is empty") {
		t.Fatalf("empty-clipboard notice = %q", notice)
	}
}

func TestPasteCommandFocusesChat(t *testing.T) {
	prev := pasteFromClipboard
	pasteFromClipboard = func() (string, error) { return "from clipboard", nil }
	t.Cleanup(func() { pasteFromClipboard = prev })

	m := newTestVaultModel(t)
	tm, _ := m.runEx("paste")
	m = tm.(VaultModel)
	if m.focus != paneChat {
		t.Fatalf(":paste should focus the chat pane, got %v", m.focus)
	}
	if got := m.chat.input.Value(); got != "from clipboard" {
		t.Fatalf("chat input = %q", got)
	}
}

func TestChatInputHistory(t *testing.T) {
	c := newChat()
	c.setSize(60, 12)
	up := tea.KeyMsg{Type: tea.KeyUp}
	down := tea.KeyMsg{Type: tea.KeyDown}

	c.input.SetValue("one")
	c.submit()
	c.input.SetValue("two")
	c.submit()

	// Empty input: up recalls the latest, then the one before.
	c.histKey(up)
	if got := c.input.Value(); got != "two" {
		t.Fatalf("after up: %q", got)
	}
	c.histKey(up)
	if got := c.input.Value(); got != "one" {
		t.Fatalf("after up up: %q", got)
	}
	// Down walks back toward the (empty) live draft.
	c.histKey(down)
	if got := c.input.Value(); got != "two" {
		t.Fatalf("after down: %q", got)
	}
	c.histKey(down)
	if got := c.input.Value(); got != "" {
		t.Fatalf("back to live draft: %q", got)
	}

	// A typed draft is preserved across a recall round-trip.
	c.input.SetValue("")
	c.draft = ""
	c.histPos = len(c.inputHist)
	c.input.SetValue("") // empty -> navigable
	c.histKey(up)        // "two"
	c.histKey(down)      // live again
	if got := c.input.Value(); got != "" {
		t.Fatalf("draft restore: %q", got)
	}

	// While composing (non-empty, not a recalled entry), arrows are NOT history.
	c.input.SetValue("half-typed question")
	if c.histKey(up) {
		t.Fatal("up while composing must remain a cursor movement")
	}
	// Submitting dedupes consecutive repeats.
	c.input.SetValue("two")
	c.submit()
	if n := len(c.inputHist); n != 2 {
		t.Fatalf("consecutive duplicate should not be re-added: %v", c.inputHist)
	}
}

func TestLastFence(t *testing.T) {
	if _, ok := lastFence("no code here"); ok {
		t.Fatal("prose has no fence")
	}
	if code, ok := lastFence("```\nunterminated\nfence"); !ok || code != "unterminated\nfence" {
		t.Fatalf("unterminated fence: %q ok=%v", code, ok)
	}
}

func TestAltOCopiesInChatPane(t *testing.T) {
	got := stubClipboard(t)
	m := newTestVaultModel(t)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")}) // noop key first
	m = tm.(VaultModel)
	m.setFocus(paneChat)
	m.chat.append(roleTutor, "copy me")

	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("ø")},            // mac Option+O default
		{Type: tea.KeyRunes, Runes: []rune("o"), Alt: true}, // alt+o
	} {
		*got = ""
		tm, _ = m.Update(k)
		m = tm.(VaultModel)
		if *got != "copy me" {
			t.Fatalf("key %q should copy the last reply, got %q", k.String(), *got)
		}
	}
}

func TestVaultPerNoteTutorHistorySeparate(t *testing.T) {
	m := newTestVaultModel(t)
	a := vSaveOpenCmd(m.svc, "x/A.md", "# A\n")().(vOpenedMsg)
	tm, _ := m.Update(a)
	m = tm.(VaultModel)
	m.chatHist = append(m.chatHist, vChatTurn("user", "about A")...)

	b := vSaveOpenCmd(m.svc, "x/B.md", "# B\n")().(vOpenedMsg)
	tm, _ = m.Update(b)
	m = tm.(VaultModel)
	if len(m.chatHist) != 0 {
		t.Fatalf("note B should start with an empty tutor conversation, got %d turns", len(m.chatHist))
	}

	tm, _ = m.Update(vOpenCmd(m.svc, "x/A.md")().(vOpenedMsg))
	m = tm.(VaultModel)
	if len(m.chatHist) != 1 || m.chatHist[0].Content != "about A" {
		t.Fatalf("note A's tutor conversation should be restored, got %+v", m.chatHist)
	}
}

// Assistant prose is markdown and must arrive styled in the transcript
// (headings, bold, inline code, wikilinks) — not as flat body text.
func TestChatRendersMarkdownProse(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(old)

	c := newChat()
	c.setSize(60, 20)
	c.append(roleTutor, "# Heading\n\nuse **bold** and ****strong**** and `code` with [[Link]]")
	content := c.renderBlock(c.blocks[0])
	// Expected SGR codes are the theme palette (internal/theme) downsampled to
	// ANSI-256: heading=Blue, bold=Text, strong=Mauve, code=Peach, link=Sapphire.
	for what, want := range map[string]string{
		"heading":  "\x1b[1;38;5;111m# Heading",
		"bold":     "\x1b[1;38;5;189m**bold**",
		"strong":   "\x1b[1;38;5;183m****strong****",
		"code":     "\x1b[38;5;216m`code`",
		"wikilink": "\x1b[38;5;117m[[Link]]",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("%s not styled in chat prose:\n%q", what, content)
		}
	}
}

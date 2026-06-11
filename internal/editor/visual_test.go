package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func esc() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }

func TestVisualCharwiseDeleteAndPaste(t *testing.T) {
	m := New("alpha beta", true, nil)
	m.SetSize(40, 10)
	// v then e selects "alpha" (inclusive); d removes it into the register.
	m = apply(m, key("v"), key("e"), key("d"))
	if got := m.Value(); got != " beta" {
		t.Fatalf("after v e d: %q", got)
	}
	if m.register != "alpha" || m.regLinewise {
		t.Fatalf("register = %q linewise=%v", m.register, m.regLinewise)
	}
	if m.mode != modeNormal {
		t.Fatal("d should leave Visual mode")
	}
	// p pastes the charwise register after the cursor (on the leading space).
	m = apply(m, key("p"))
	if got := m.Value(); got != " alphabeta" {
		t.Fatalf("after p: %q", got)
	}
}

// ":" from Visual mode opens the command line and captures the selection, so a
// forwarded (parent) command carries the selected span.
func TestVisualColonForwardsSelection(t *testing.T) {
	m := New("alpha beta gamma", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"), key("e"), key(":")) // select "alpha", open ":"
	if m.mode != modeCommand {
		t.Fatalf("’:’ in Visual should open the command line, mode=%v", m.mode)
	}
	m = apply(m, key("edit make it formal")) // type into the command line

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("Enter should forward the unknown command")
	}
	msg, ok := cmd().(RunCommandMsg)
	if !ok {
		t.Fatalf("expected RunCommandMsg, got %T", cmd())
	}
	if msg.Raw != "edit make it formal" {
		t.Fatalf("Raw = %q", msg.Raw)
	}
	if msg.Sel == nil || msg.Sel.Text != "alpha" {
		t.Fatalf("selection not carried: %+v", msg.Sel)
	}
}

// Esc out of a Visual ":" drops the captured selection; a later Normal-mode ":"
// command carries none.
func TestVisualColonEscDropsSelection(t *testing.T) {
	m := New("alpha beta", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"), key("e"), key(":"), esc())
	if m.selCapture != nil {
		t.Fatal("Esc should drop the captured selection")
	}
	m = apply(m, key(":"))
	m = apply(m, key("progress"))
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = tm
	if msg, ok := cmd().(RunCommandMsg); !ok || msg.Sel != nil {
		t.Fatalf("a Normal-mode command must carry no selection: %+v", msg)
	}
}

// ",d" from Visual mode forwards a :decode command carrying the selection, the
// same path ":decode" takes — d-code's shortcut for decoding selected lines.
func TestVisualCommaDecodeForwardsSelection(t *testing.T) {
	m := New("alpha beta gamma", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"), key("e")) // select "alpha"

	tm, _ := m.Update(key(","))
	m = tm.(Model)
	tm, cmd := m.Update(key("d"))
	m = tm.(Model)
	if cmd == nil {
		t.Fatal(",d should forward a decode command")
	}
	msg, ok := cmd().(RunCommandMsg)
	if !ok {
		t.Fatalf("expected RunCommandMsg, got %T", cmd())
	}
	if msg.Raw != "decode" {
		t.Fatalf("Raw = %q, want decode", msg.Raw)
	}
	if msg.Sel == nil || msg.Sel.Text != "alpha" {
		t.Fatalf("selection not carried: %+v", msg.Sel)
	}
	if m.mode != modeNormal {
		t.Fatal(",d should leave Visual mode")
	}
}

func TestReplaceRangeVerifiesAndUndoes(t *testing.T) {
	m := New("alpha beta gamma", true, nil)
	m.SetSize(40, 10)

	if !(&m).ReplaceRange(6, 10, "BRAVO", "beta") { // "beta" -> "BRAVO"
		t.Fatal("ReplaceRange should succeed when the span matches")
	}
	if got := m.Value(); got != "alpha BRAVO gamma" {
		t.Fatalf("after ReplaceRange: %q", got)
	}
	// A stale span (want no longer matches) is refused without changing text.
	if (&m).ReplaceRange(0, 5, "X", "WRONG") {
		t.Fatal("ReplaceRange should refuse a mismatched span")
	}
	if got := m.Value(); got != "alpha BRAVO gamma" {
		t.Fatalf("refused replace must not change the buffer: %q", got)
	}
	// The successful replace is one undoable edit.
	m = apply(m, key("u"))
	if got := m.Value(); got != "alpha beta gamma" {
		t.Fatalf("u should restore the pre-replace text: %q", got)
	}
}

func TestVisualReverseSelection(t *testing.T) {
	m := New("abcd", true, nil)
	m.SetSize(40, 10)
	// Move right twice, then select backwards to the start: span is c..a.
	m = apply(m, key("l"), key("l"), key("v"), key("h"), key("h"), key("d"))
	if got := m.Value(); got != "d" {
		t.Fatalf("reverse selection delete: %q", got)
	}
	if m.register != "abc" {
		t.Fatalf("register = %q", m.register)
	}
}

func TestVisualLinewiseDeleteAndPaste(t *testing.T) {
	m := New("one\ntwo\nthree", true, nil)
	m.SetSize(40, 10)
	// V j selects the first two lines; d removes them linewise.
	m = apply(m, key("V"), key("j"), key("d"))
	if got := m.Value(); got != "three" {
		t.Fatalf("after V j d: %q", got)
	}
	if m.register != "one\ntwo" || !m.regLinewise {
		t.Fatalf("register = %q linewise=%v", m.register, m.regLinewise)
	}
	m = apply(m, key("p"))
	if got := m.Value(); got != "three\none\ntwo" {
		t.Fatalf("after p: %q", got)
	}
}

func TestVisualYankLeavesBufferAndMovesToStart(t *testing.T) {
	m := New("alpha beta", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("w"), key("v"), key("e"), key("y"))
	if got := m.Value(); got != "alpha beta" {
		t.Fatalf("y must not change the buffer: %q", got)
	}
	if m.register != "beta" || m.regLinewise {
		t.Fatalf("register = %q linewise=%v", m.register, m.regLinewise)
	}
	// Vim leaves the cursor at the selection start ('b' of beta).
	if got := insertProbe(m).Value(); got != "alpha Xbeta" {
		t.Fatalf("cursor after y: %q", got)
	}
}

func TestVisualChangeEntersInsert(t *testing.T) {
	m := New("alpha beta", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"), key("e"), key("c"))
	if m.mode != modeInsert {
		t.Fatal("c should enter Insert mode")
	}
	m = apply(m, key("X"))
	if got := m.Value(); got != "X beta" {
		t.Fatalf("after v e c X: %q", got)
	}
}

func TestVisualLinewiseChangeKeepsEmptyLine(t *testing.T) {
	m := New("one\ntwo", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("V"), key("c"), key("X"))
	if got := m.Value(); got != "X\ntwo" {
		t.Fatalf("after V c X: %q", got)
	}
}

func TestVisualIndentSelectedLines(t *testing.T) {
	m := New("one\ntwo\nthree", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"), key("j"), key(">"))
	want := tabIndent + "one\n" + tabIndent + "two\nthree"
	if got := m.Value(); got != want {
		t.Fatalf("after v j >: %q, want %q", got, want)
	}
	if m.mode != modeNormal {
		t.Fatal("> should leave Visual mode")
	}
	// And back with <.
	m = apply(m, key("v"), key("j"), key("<"))
	if got := m.Value(); got != "one\ntwo\nthree" {
		t.Fatalf("after v j <: %q", got)
	}
}

func TestVisualEscapeAbandonsSelection(t *testing.T) {
	m := New("alpha", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"), key("e"), esc())
	if m.mode != modeNormal {
		t.Fatal("Esc should return to Normal")
	}
	if got := m.Value(); got != "alpha" {
		t.Fatalf("Esc must not modify the buffer: %q", got)
	}
}

func TestVisualToggleAndSwitch(t *testing.T) {
	m := New("alpha", true, nil)
	m.SetSize(40, 10)
	// v…v exits; v…V switches to linewise.
	m = apply(m, key("v"), key("v"))
	if m.mode != modeNormal {
		t.Fatal("v v should exit Visual")
	}
	m = apply(m, key("v"), key("V"))
	if m.mode != modeVisual || !m.visualLine {
		t.Fatal("v V should switch to linewise Visual")
	}
	m = apply(m, key("V"))
	if m.mode != modeNormal {
		t.Fatal("V in V-LINE should exit")
	}
}

func TestVisualSwapEnds(t *testing.T) {
	m := New("abcd", true, nil)
	m.SetSize(40, 10)
	// v l puts cursor on 'b' with anchor at 'a'; o swaps so the cursor is back
	// on 'a' and the anchor on 'b'.
	m = apply(m, key("v"), key("l"), key("o"), esc())
	if got := insertProbe(m).Value(); got != "Xabcd" {
		t.Fatalf("cursor after o: %q", got)
	}
}

func TestVisualGGMotion(t *testing.T) {
	m := New("one\ntwo\nthree", true, nil)
	m.SetSize(40, 10)
	// From the last line, V then gg selects everything; d empties the buffer.
	m = apply(m, key("G"), key("V"), key("g"), key("g"), key("d"))
	if got := m.Value(); got != "" {
		t.Fatalf("after G V gg d: %q", got)
	}
}

func TestVisualViewHighlightsSelection(t *testing.T) {
	forceColor(t)
	m := New("abc", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"), key("l")) // 'a' selected, cursor on 'b'
	view := m.visualView()
	if !strings.Contains(view, visualSelStyle.Render("a")) {
		t.Fatalf("selection should be highlighted:\n%q", view)
	}
	if !strings.Contains(view, visualCursorStyle.Render("b")) {
		t.Fatalf("cursor should be drawn:\n%q", view)
	}
}

func TestVisualBadgeInStatusLine(t *testing.T) {
	m := New("abc", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"))
	if !strings.Contains(m.statusLine(), "VISUAL") {
		t.Fatalf("status should show VISUAL: %q", m.statusLine())
	}
	m = apply(m, key("V"))
	if !strings.Contains(m.statusLine(), "V-LINE") {
		t.Fatalf("status should show V-LINE: %q", m.statusLine())
	}
}

func TestWrapWidths(t *testing.T) {
	// 10 runes, width 4 -> 3 segments.
	segs := wrapWidths([]rune("abcdefghij"), 4)
	want := [][2]int{{0, 4}, {4, 8}, {8, 10}}
	if len(segs) != len(want) {
		t.Fatalf("segs = %v", segs)
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Fatalf("segs[%d] = %v, want %v", i, segs[i], want[i])
		}
	}
	// Wide (CJK) runes count double.
	segs = wrapWidths([]rune("한국어로"), 4)
	if len(segs) != 2 || segs[0] != [2]int{0, 2} {
		t.Fatalf("CJK segs = %v", segs)
	}
	// Empty line still yields one segment.
	if segs := wrapWidths(nil, 4); len(segs) != 1 {
		t.Fatalf("empty segs = %v", segs)
	}
}

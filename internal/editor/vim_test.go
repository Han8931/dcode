package editor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// insertProbe types "i X <esc>" so the buffer reveals where the cursor was:
// the X lands immediately before the character the cursor sat on.
func insertProbe(m Model) Model {
	return apply(m, key("i"), key("X"), tea.KeyMsg{Type: tea.KeyEsc})
}

func TestVimWMovesToNextWordStart(t *testing.T) {
	m := New("alpha beta gamma", true, nil)
	m.SetSize(40, 10)
	// w from 'a' of alpha must land on 'b' of beta (NOT the end of alpha).
	m = apply(m, key("w"))
	if got := insertProbe(m).Value(); got != "alpha Xbeta gamma" {
		t.Fatalf("w landed wrong: %q", got)
	}
}

func TestVimWCrossesLines(t *testing.T) {
	m := New("one\ntwo", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("w"))
	if got := insertProbe(m).Value(); got != "one\nXtwo" {
		t.Fatalf("w should cross to the next line's word: %q", got)
	}
}

func TestVimEMovesToWordEnd(t *testing.T) {
	m := New("alpha beta", true, nil)
	m.SetSize(40, 10)
	// e from 'a' of alpha lands on the last char of alpha.
	m = apply(m, key("e"))
	if got := insertProbe(m).Value(); got != "alphXa beta" {
		t.Fatalf("e landed wrong: %q", got)
	}
}

func TestVimBMovesToPreviousWordStart(t *testing.T) {
	m := New("alpha beta", true, nil)
	m.SetSize(40, 10)
	// $ to end, then b lands on 'b' of beta.
	m = apply(m, key("$"), key("b"))
	if got := insertProbe(m).Value(); got != "alpha Xbeta" {
		t.Fatalf("b landed wrong: %q", got)
	}
}

func TestVimDeleteLineThenPaste(t *testing.T) {
	m := New("one\ntwo\nthree", true, nil)
	m.SetSize(40, 10)
	// dd captures "one" linewise; cursor is now on "two"; p pastes below it.
	m = apply(m, key("d"), key("d"))
	if got := m.Value(); got != "two\nthree" {
		t.Fatalf("after dd: %q", got)
	}
	if m.register != "one" || !m.regLinewise {
		t.Fatalf("register = %q linewise=%v, want \"one\" linewise", m.register, m.regLinewise)
	}
	m = apply(m, key("p"))
	if got := m.Value(); got != "two\none\nthree" {
		t.Fatalf("after p: %q", got)
	}
}

func TestVimYankThenPaste(t *testing.T) {
	m := New("alpha\nbeta", true, nil)
	m.SetSize(40, 10)
	// yy leaves the buffer intact and p duplicates the line below.
	m = apply(m, key("y"), key("y"))
	if got := m.Value(); got != "alpha\nbeta" {
		t.Fatalf("yy must not modify the buffer: %q", got)
	}
	m = apply(m, key("p"))
	if got := m.Value(); got != "alpha\nalpha\nbeta" {
		t.Fatalf("after yy p: %q", got)
	}
}

func TestVimUppercasePPastesAbove(t *testing.T) {
	m := New("alpha\nbeta", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("y"), key("y"), key("j"), key("P"))
	if got := m.Value(); got != "alpha\nalpha\nbeta" {
		t.Fatalf("after yy j P: %q", got)
	}
}

func TestVimXCapturesCharwiseAndPastes(t *testing.T) {
	m := New("abc", true, nil)
	m.SetSize(40, 10)
	// x deletes 'a' into the register; p pastes it after the cursor (now on 'b').
	m = apply(m, key("x"))
	if got := m.Value(); got != "bc" {
		t.Fatalf("after x: %q", got)
	}
	if m.register != "a" || m.regLinewise {
		t.Fatalf("register = %q linewise=%v, want charwise \"a\"", m.register, m.regLinewise)
	}
	m = apply(m, key("p"))
	if got := m.Value(); got != "bac" {
		t.Fatalf("after x p: %q", got)
	}
}

func TestTabIndentsInInsertMode(t *testing.T) {
	m := New("x", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("i"), tea.KeyMsg{Type: tea.KeyTab})
	if got := m.Value(); got != tabIndent+"x" {
		t.Fatalf("tab should indent in Insert mode: %q", got)
	}
}

func TestTabIndentsInDefaultMode(t *testing.T) {
	m := New("x", false, nil)
	m.SetSize(40, 10)
	m = apply(m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.Value(); got != tabIndent+"x" {
		t.Fatalf("tab should indent in default mode: %q", got)
	}
}

func TestTabIgnoredInNormalMode(t *testing.T) {
	m := New("x", true, nil)
	m.SetSize(40, 10)
	m = apply(m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.Value(); got != "x" {
		t.Fatalf("tab in Normal mode must not edit the buffer: %q", got)
	}
}

func TestVimIndentAndDedentLine(t *testing.T) {
	m := New("alpha\nbeta", true, nil)
	m.SetSize(40, 10)

	// >> indents the current line by one level.
	m = apply(m, key(">"), key(">"))
	if got := m.Value(); got != tabIndent+"alpha\nbeta" {
		t.Fatalf("after >>: %q", got)
	}
	// << removes it again; the other line is untouched.
	m = apply(m, key("<"), key("<"))
	if got := m.Value(); got != "alpha\nbeta" {
		t.Fatalf("after <<: %q", got)
	}
}

func TestVimDedentPartialAndTab(t *testing.T) {
	// Two leading spaces: << removes both (less than a full level).
	m := New("  alpha", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("<"), key("<"))
	if got := m.Value(); got != "alpha" {
		t.Fatalf("partial dedent: %q", got)
	}
	// A leading tab counts as one level.
	m = New("\talpha", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("<"), key("<"))
	if got := m.Value(); got != "alpha" {
		t.Fatalf("tab dedent: %q", got)
	}
	// Unindented line: << is a no-op.
	m = New("alpha", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("<"), key("<"))
	if got := m.Value(); got != "alpha" {
		t.Fatalf("no-op dedent changed buffer: %q", got)
	}
}

func TestVimIndentOnlyCurrentLine(t *testing.T) {
	m := New("one\ntwo\nthree", true, nil)
	m.SetSize(40, 10)
	// Move to the middle line, indent it, and confirm neighbors are untouched
	// and the cursor stayed on that line.
	m = apply(m, key("j"), key(">"), key(">"))
	if got := m.Value(); got != "one\n"+tabIndent+"two\nthree" {
		t.Fatalf("after j >>: %q", got)
	}
	if got := insertProbe(m).Value(); got != "one\n"+tabIndent+"Xtwo\nthree" {
		t.Fatalf("cursor should stay on 'two': %q", got)
	}
}

func TestRemovedChunk(t *testing.T) {
	cases := []struct{ before, after, want string }{
		{"one\ntwo\nthree", "two\nthree", "one\n"},
		{"abc", "bc", "a"},
		{"abc", "ab", "c"},
		{"aXXa", "aa", "XX"},
		{"same", "same", ""},
	}
	for _, c := range cases {
		got, _ := removedChunk(c.before, c.after)
		if got != c.want {
			t.Errorf("removedChunk(%q,%q) = %q, want %q", c.before, c.after, got, c.want)
		}
	}
}

func TestWordMotionMath(t *testing.T) {
	runes := []rune("foo bar\nbaz")
	if got := nextWordStart(runes, 0); got != 4 { // 'b' of bar
		t.Errorf("nextWordStart(0) = %d", got)
	}
	if got := nextWordStart(runes, 4); got != 8 { // 'b' of baz (across \n)
		t.Errorf("nextWordStart(4) = %d", got)
	}
	if got := nextWordEnd(runes, 0); got != 2 { // last 'o' of foo
		t.Errorf("nextWordEnd(0) = %d", got)
	}
	if got := prevWordStart(runes, 8); got != 4 { // back to 'b' of bar
		t.Errorf("prevWordStart(8) = %d", got)
	}
	if got := prevWordStart(runes, 1); got != 0 {
		t.Errorf("prevWordStart(1) = %d", got)
	}
}

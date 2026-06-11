package editor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func enter() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

func TestCountedMotions(t *testing.T) {
	m := New("aa bb cc dd ee", true, nil)
	m.SetSize(60, 10)
	// 3w lands on the start of the 4th word.
	m = apply(m, key("3"), key("w"))
	if got := insertProbe(m).Value(); got != "aa bb cc Xdd ee" {
		t.Fatalf("3w landed wrong: %q", got)
	}
}

func TestCountedDeleteChar(t *testing.T) {
	m := New("abcdef", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("3"), key("x"))
	if got := m.Value(); got != "def" {
		t.Fatalf("3x: %q", got)
	}
	if m.register != "abc" {
		t.Fatalf("register after 3x = %q", m.register)
	}
}

func TestCountedDeleteLines(t *testing.T) {
	m := New("one\ntwo\nthree\nfour", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("2"), key("d"), key("d"))
	if got := m.Value(); got != "three\nfour" {
		t.Fatalf("2dd: %q", got)
	}
	if m.register != "one\ntwo" || !m.regLinewise {
		t.Fatalf("register after 2dd = %q linewise=%v", m.register, m.regLinewise)
	}
	// And undo restores both lines at once.
	m = apply(m, key("u"))
	if got := m.Value(); got != "one\ntwo\nthree\nfour" {
		t.Fatalf("undo 2dd: %q", got)
	}
}

func TestCountedYankAndPaste(t *testing.T) {
	m := New("one\ntwo\nthree", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("2"), key("y"), key("y"), key("p"))
	if got := m.Value(); got != "one\none\ntwo\ntwo\nthree" {
		t.Fatalf("2yy p: %q", got)
	}
}

func TestCountedIndent(t *testing.T) {
	m := New("a\nb\nc", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("2"), key(">"), key(">"))
	if got := m.Value(); got != tabIndent+"a\n"+tabIndent+"b\nc" {
		t.Fatalf("2>>: %q", got)
	}
}

func TestZeroIsMotionUnlessCounting(t *testing.T) {
	m := New("abc def", true, nil)
	m.SetSize(60, 10)
	// $ then 0 returns to line start (motion).
	m = apply(m, key("$"), key("0"))
	if got := insertProbe(m).Value(); got != "Xabc def" {
		t.Fatalf("0 as motion: %q", got)
	}
	// 1 then 0 makes a count of 10 (digit), consumed by the next motion.
	m2 := New("a b c d e f g h i j k l", true, nil)
	m2.SetSize(60, 10)
	m2 = apply(m2, key("1"), key("0"), key("w"))
	if got := insertProbe(m2).Value(); got != "a b c d e f g h i j Xk l" {
		t.Fatalf("10w: %q", got)
	}
}

func TestEscClearsCount(t *testing.T) {
	m := New("abcdef", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("3"), esc(), key("x"))
	if got := m.Value(); got != "bcdef" {
		t.Fatalf("count must reset on Esc: %q", got)
	}
}

func TestFindChar(t *testing.T) {
	m := New("alpha beta gamma", true, nil)
	m.SetSize(60, 10)
	// fm jumps to the first 'm' on the line.
	m = apply(m, key("f"), key("m"))
	if got := insertProbe(m).Value(); got != "alpha beta gaXmma" {
		t.Fatalf("fm landed wrong: %q", got)
	}
}

func TestFindCharTillAndBack(t *testing.T) {
	// tX stops just before the X (on 'c').
	m2 := New("abcXdef", true, nil)
	m2.SetSize(60, 10)
	m2 = apply(m2, key("t"), key("X"))
	if got := insertProbe(m2).Value(); got != "abXcXdef" {
		t.Fatalf("tX landed wrong: %q", got)
	}
	// FX from the end searches backward.
	m3 := New("abcXdef", true, nil)
	m3.SetSize(60, 10)
	m3 = apply(m3, key("$"), key("F"), key("X"))
	if got := insertProbe(m3).Value(); got != "abcXXdef" {
		t.Fatalf("FX landed wrong: %q", got)
	}
}

func TestFindRepeatSemicolonComma(t *testing.T) {
	m := New("a.b.c.d", true, nil)
	m.SetSize(60, 10)
	// f. then ; advances to the second dot; , goes back to the first.
	// (Assert via cursorPos — insertProbe mutates shared textarea state, so it
	// can't be used mid-sequence.)
	m = apply(m, key("f"), key("."), key(";"))
	if _, col := m.cursorPos(); col != 3 {
		t.Fatalf("f. ; cursor col = %d, want 3", col)
	}
	m = apply(m, key(","))
	if _, col := m.cursorPos(); col != 1 {
		t.Fatalf(", cursor col = %d, want 1", col)
	}
}

func TestJoinLines(t *testing.T) {
	m := New("hello\n    world\nrest", true, nil)
	m.SetSize(60, 10)
	// J joins and collapses the next line's leading indentation to one space.
	m = apply(m, key("J"))
	if got := m.Value(); got != "hello world\nrest" {
		t.Fatalf("J: %q", got)
	}
	// Count: 2J joins twice.
	m2 := New("a\nb\nc", true, nil)
	m2.SetSize(60, 10)
	m2 = apply(m2, key("2"), key("J"))
	if got := m2.Value(); got != "a b c" {
		t.Fatalf("2J: %q", got)
	}
	// Undoable.
	m2 = apply(m2, key("u"))
	if got := m2.Value(); got != "a\nb\nc" {
		t.Fatalf("undo 2J: %q", got)
	}
}

func TestToggleCase(t *testing.T) {
	m := New("aBc", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("3"), key("~"))
	if got := m.Value(); got != "AbC" {
		t.Fatalf("3~: %q", got)
	}
}

func TestSearchAndRepeat(t *testing.T) {
	m := New("alpha\nbeta\nalpha again\nomega", true, nil)
	m.SetSize(60, 10)
	// /alpha finds the NEXT occurrence (wrapping past the cursor's own line).
	m = apply(m, key("/"))
	if m.mode != modeCommand {
		t.Fatal("/ should open the search prompt")
	}
	m = apply(m, key("a"), key("l"), key("p"), key("h"), key("a"), enter())
	if m.mode != modeNormal {
		t.Fatal("search should return to Normal mode")
	}
	if row, col := m.cursorPos(); row != 2 || col != 0 {
		t.Fatalf("/alpha cursor = (%d,%d), want (2,0)", row, col)
	}
	// n wraps around to the first occurrence.
	m = apply(m, key("n"))
	if row, col := m.cursorPos(); row != 0 || col != 0 {
		t.Fatalf("n cursor = (%d,%d), want (0,0)", row, col)
	}
	// N searches backward (wrapping back to the later occurrence).
	m = apply(m, key("N"))
	if row, col := m.cursorPos(); row != 2 || col != 0 {
		t.Fatalf("N cursor = (%d,%d), want (2,0)", row, col)
	}
}

func TestXAtEndOfLineCutsLastChar(t *testing.T) {
	// The reported bug: after $ (or leaving Insert at EOL) the cursor sat PAST
	// the last char, so x ate the newline instead of cutting a character.
	m := New("abc\ndef", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("$"), key("x"))
	if got := m.Value(); got != "ab\ndef" {
		t.Fatalf("$ x should cut the last char: %q", got)
	}
	if m.register != "c" {
		t.Fatalf("register = %q, want \"c\"", m.register)
	}
}

func TestXAfterInsertEscCutsTypedChar(t *testing.T) {
	m := New("", true, nil)
	m.SetSize(60, 10)
	// Type "hi", Esc (cursor must land ON 'i', Vim-style), then x cuts it.
	m = apply(m, key("i"), key("h"), key("i"), esc(), key("x"))
	if got := m.Value(); got != "h" {
		t.Fatalf("i hi <esc> x: %q", got)
	}
}

func TestXNeverEatsNewline(t *testing.T) {
	m := New("ab\ncd", true, nil)
	m.SetSize(60, 10)
	// 9x from 'a': cuts at most to the end of the line, never joining lines.
	m = apply(m, key("9"), key("x"))
	if got := m.Value(); got != "\ncd" {
		t.Fatalf("9x: %q", got)
	}
	if m.register != "ab" {
		t.Fatalf("register = %q", m.register)
	}
	// x on the now-empty line is a no-op (as in Vim).
	m = apply(m, key("x"))
	if got := m.Value(); got != "\ncd" {
		t.Fatalf("x on empty line must be a no-op: %q", got)
	}
}

func TestTildeAtEndOfLine(t *testing.T) {
	m := New("abc", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("$"), key("~"))
	if got := m.Value(); got != "abC" {
		t.Fatalf("$ ~ should toggle the last char: %q", got)
	}
}

func TestSearchNotFound(t *testing.T) {
	m := New("hello", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("/"), key("z"), key("q"), enter())
	if got := m.Value(); got != "hello" {
		t.Fatalf("failed search must not modify the buffer: %q", got)
	}
	if m.status == "" {
		t.Fatal("failed search should report in the status line")
	}
}

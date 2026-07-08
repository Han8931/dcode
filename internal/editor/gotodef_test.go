package editor

import "testing"

func TestWordUnderCursor(t *testing.T) {
	m := New("alpha beta gamma", true, nil)
	m.SetSize(60, 10)

	// Cursor starts on the first word.
	if got := m.WordUnderCursor(); got != "alpha" {
		t.Fatalf("WordUnderCursor at start = %q, want alpha", got)
	}
	// w moves to the next word; the cursor now sits on it.
	m = apply(m, key("w"))
	if got := m.WordUnderCursor(); got != "beta" {
		t.Fatalf("WordUnderCursor after w = %q, want beta", got)
	}
}

func TestWordUnderCursorOffIdentifier(t *testing.T) {
	m := New("  x", true, nil) // cursor on a leading space, not an identifier
	m.SetSize(60, 10)
	if got := m.WordUnderCursor(); got != "" {
		t.Fatalf("WordUnderCursor off an identifier = %q, want empty", got)
	}
}

func TestGdForwardsDefCommand(t *testing.T) {
	m := New("helper\n", true, nil)
	m.SetSize(60, 10)

	m = apply(m, key("g")) // pending g
	tm, cmd := m.Update(key("d"))
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("gd on a symbol should emit a command")
	}
	rc, ok := cmd().(RunCommandMsg)
	if !ok || rc.Raw != "def helper" {
		t.Fatalf("gd should forward RunCommandMsg{Raw:\"def helper\"}, got %#v", cmd())
	}
}

func TestGotoLineMovesCursor(t *testing.T) {
	m := New("l1\nl2\nl3\nl4", true, nil)
	m.SetSize(60, 10)
	m.GotoLine(3)
	if row, _ := m.cursorPos(); row != 2 {
		t.Fatalf("GotoLine(3) put cursor on row %d, want 2", row)
	}
}

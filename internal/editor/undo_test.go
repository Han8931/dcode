package editor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlR() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlR} }

// ReplaceAll swaps the whole buffer as one undoable edit (used to apply an AI
// rewrite): u restores the previous text, unlike SetValue which clears history.
func TestReplaceAllIsUndoable(t *testing.T) {
	m := New("# notes\n\nrough draft", true, nil)
	m.SetSize(40, 10)

	m.ReplaceAll("# Notes\n\nPolished draft.")
	if got := m.Value(); got != "# Notes\n\nPolished draft." {
		t.Fatalf("ReplaceAll: %q", got)
	}
	m = apply(m, key("u"))
	if got := m.Value(); got != "# notes\n\nrough draft" {
		t.Fatalf("u should restore the pre-replace note: %q", got)
	}
}

func TestUndoRedoDeleteLine(t *testing.T) {
	m := New("one\ntwo\nthree", true, nil)
	m.SetSize(40, 10)

	m = apply(m, key("d"), key("d"))
	if got := m.Value(); got != "two\nthree" {
		t.Fatalf("after dd: %q", got)
	}
	m = apply(m, key("u"))
	if got := m.Value(); got != "one\ntwo\nthree" {
		t.Fatalf("after u: %q", got)
	}
	m = apply(m, ctrlR())
	if got := m.Value(); got != "two\nthree" {
		t.Fatalf("after ctrl+r: %q", got)
	}
}

func TestUndoInsertSessionIsOneUnit(t *testing.T) {
	m := New("base", true, nil)
	m.SetSize(40, 10)
	// One insert session types several characters; a single u removes them all.
	m = apply(m, key("i"), key("X"), key("Y"), key("Z"), tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.Value(); got != "XYZbase" {
		t.Fatalf("after insert: %q", got)
	}
	m = apply(m, key("u"))
	if got := m.Value(); got != "base" {
		t.Fatalf("one undo should revert the whole session: %q", got)
	}
}

func TestUndoAtOldestIsSafe(t *testing.T) {
	m := New("text", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("u"), key("u"))
	if got := m.Value(); got != "text" {
		t.Fatalf("undo with no history must not change the buffer: %q", got)
	}
}

func TestUndoVisualDeleteAndIndent(t *testing.T) {
	m := New("alpha beta", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("v"), key("e"), key("d"))
	if got := m.Value(); got != " beta" {
		t.Fatalf("after v e d: %q", got)
	}
	m = apply(m, key("u"))
	if got := m.Value(); got != "alpha beta" {
		t.Fatalf("undo visual delete: %q", got)
	}

	m = apply(m, key(">"), key(">"), key("u"))
	if got := m.Value(); got != "alpha beta" {
		t.Fatalf("undo >>: %q", got)
	}
}

func TestNewChangeInvalidatesRedo(t *testing.T) {
	m := New("one\ntwo", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("d"), key("d"), key("u")) // delete, undo
	m = apply(m, key("x"))                     // new change
	m = apply(m, ctrlR())                      // redo must be dead now
	if got := m.Value(); got != "ne\ntwo" {
		t.Fatalf("redo after a new change must do nothing: %q", got)
	}
}

func TestSetValueClearsHistory(t *testing.T) {
	m := New("first", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("x")) // create history on the first buffer
	m.SetValue("second")
	m = apply(m, key("u"))
	if got := m.Value(); got != "second" {
		t.Fatalf("undo must not cross buffers: %q", got)
	}
}

func TestOpenBelowAutoIndents(t *testing.T) {
	m := New("    indented", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("o"), key("x"))
	if got := m.Value(); got != "    indented\n    x" {
		t.Fatalf("o should open at the same indentation: %q", got)
	}
	// And the whole o+typing session is one undo unit.
	m = apply(m, tea.KeyMsg{Type: tea.KeyEsc}, key("u"))
	if got := m.Value(); got != "    indented" {
		t.Fatalf("undo after o: %q", got)
	}
}

func TestOpenAboveAutoIndents(t *testing.T) {
	// Note: the textarea sanitizes tabs to spaces on input, so indentation is
	// space-based by the time o/O sees it.
	m := New("  deep", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("O"), key("y"))
	if got := m.Value(); got != "  y\n  deep" {
		t.Fatalf("O should open above at the same indentation: %q", got)
	}
}

func TestEditorCommandHistory(t *testing.T) {
	m := New("", true, nil)
	m.SetSize(60, 10)
	// Run two ex-commands (unknown ones are forwarded to the parent, but they
	// still enter the history).
	m = apply(m, key(":"))
	for _, r := range "progress" {
		m = apply(m, key(string(r)))
	}
	m = apply(m, enter())
	m = apply(m, key(":"))
	for _, r := range "w" {
		m = apply(m, key(string(r)))
	}
	m = apply(m, enter())

	// Recall via ↑ in a fresh prompt.
	m = apply(m, key(":"), tea.KeyMsg{Type: tea.KeyUp})
	if got := m.cmd.Value(); got != "w" {
		t.Fatalf("↑ = %q, want \"w\"", got)
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.cmd.Value(); got != "progress" {
		t.Fatalf("↑↑ = %q, want \"progress\"", got)
	}

	// Search history is separate from ex history.
	m = apply(m, tea.KeyMsg{Type: tea.KeyEsc}, key("/"), tea.KeyMsg{Type: tea.KeyUp})
	if got := m.cmd.Value(); got != "" {
		t.Fatalf("search history should start empty, got %q", got)
	}
}

func TestEnterAutoIndentsInsideBraces(t *testing.T) {
	m := New("", true, nil)
	m.SetSize(60, 10)
	// Type a Go block: "func f() {" <Enter> "x" <Enter> "}" — the body indents
	// itself and the closing brace electrically dedents back to column 0.
	m = apply(m, key("i"))
	for _, r := range "func f() {" {
		m = apply(m, key(string(r)))
	}
	m = apply(m, enter())
	if got := m.Value(); got != "func f() {\n"+tabIndent {
		t.Fatalf("enter after {: %q", got)
	}
	m = apply(m, key("x"), enter())
	if got := m.Value(); got != "func f() {\n"+tabIndent+"x\n"+tabIndent {
		t.Fatalf("enter keeps depth: %q", got)
	}
	m = apply(m, key("}"))
	if got := m.Value(); got != "func f() {\n"+tabIndent+"x\n}" {
		t.Fatalf("electric }: %q", got)
	}
}

func TestEnterCopiesIndentWithoutOpener(t *testing.T) {
	m := New("    return x", true, nil)
	m.SetSize(60, 10)
	// A (append at end) then Enter: same depth, no deepening.
	m = apply(m, key("A"), enter(), key("y"))
	if got := m.Value(); got != "    return x\n    y" {
		t.Fatalf("plain autoindent: %q", got)
	}
}

func TestOpenBelowDeepensAfterOpener(t *testing.T) {
	m := New("if ready {", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("o"), key("x"))
	if got := m.Value(); got != "if ready {\n"+tabIndent+"x" {
		t.Fatalf("o after {: %q", got)
	}
	// Python's colon deepens too.
	m2 := New("def f():", true, nil)
	m2.SetSize(60, 10)
	m2 = apply(m2, key("o"), key("y"))
	if got := m2.Value(); got != "def f():\n"+tabIndent+"y" {
		t.Fatalf("o after colon: %q", got)
	}
}

func TestElectricCloseOnlyOnBareIndent(t *testing.T) {
	// Typing } after real text must NOT re-indent the line.
	m := New("", true, nil)
	m.SetSize(60, 10)
	m = apply(m, key("i"))
	for _, r := range "    x" {
		m = apply(m, key(string(r)))
	}
	m = apply(m, key("}"))
	if got := m.Value(); got != "    x}" {
		t.Fatalf("} after text must not dedent: %q", got)
	}
}

func TestOpenBelowNoIndentUnchanged(t *testing.T) {
	m := New("plain", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("o"), key("x"))
	if got := m.Value(); got != "plain\nx" {
		t.Fatalf("o on an unindented line: %q", got)
	}
}

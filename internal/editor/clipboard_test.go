package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Keep the real system clipboard out of every test in this package: yank
// tests would otherwise overwrite whatever the developer had copied.
func init() {
	copyToSystem = func(string) {}
}

func TestYankMirrorsToSystemClipboard(t *testing.T) {
	var copied string
	old := copyToSystem
	copyToSystem = func(s string) { copied = s }
	defer func() { copyToSystem = old }()

	m := New("alpha line\nbeta line", true, nil)
	m.SetSize(60, 6)
	press := func(s string) {
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		m = tm.(Model)
	}

	press("y")
	press("y") // yy
	if copied != "alpha line" {
		t.Fatalf("yy mirrored %q, want %q", copied, "alpha line")
	}

	// Visual yank mirrors the selection.
	press("v")
	press("l")
	press("y")
	if copied != "al" {
		t.Fatalf("visual y mirrored %q, want %q", copied, "al")
	}

	// Deletes stay register-only — they must not clobber the clipboard.
	copied = "untouched"
	press("x")
	if copied != "untouched" {
		t.Fatalf("x wrote to the system clipboard: %q", copied)
	}
	if m.register != "a" {
		t.Fatalf("x register = %q, want %q", m.register, "a")
	}
}

func TestAltVAndBracketedPaste(t *testing.T) {
	oldPaste := pasteFromSystem
	pasteFromSystem = func() (string, error) { return "CLIP", nil }
	defer func() { pasteFromSystem = oldPaste }()

	// Alt+V in Vim Normal mode inserts the clipboard at the cursor.
	m := New("start", true, nil)
	m.SetSize(60, 6)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v"), Alt: true})
	m = tm.(Model)
	if got := m.Value(); !strings.Contains(got, "CLIP") {
		t.Fatalf("alt+v did not paste: %q", got)
	}

	// A bracketed paste (terminal Cmd+V) in Normal mode lands literally in
	// the buffer instead of executing as Vim commands ("dd" here would
	// otherwise delete the line).
	m2 := New("keep this line", true, nil)
	m2.SetSize(60, 6)
	tm, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dd"), Paste: true})
	m2 = tm.(Model)
	if got := m2.Value(); !strings.Contains(got, "keep this line") || !strings.Contains(got, "dd") {
		t.Fatalf("bracketed paste mis-handled in Normal mode: %q", got)
	}
}

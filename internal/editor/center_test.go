package editor

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// firstGutterNum parses the first line number visible in a rendered view.
func firstGutterNum(t *testing.T, view string) int {
	t.Helper()
	for _, row := range strings.Split(stripANSI(view), "\n") {
		f := strings.Fields(row)
		if len(f) == 0 {
			continue
		}
		n := 0
		for _, c := range f[0] {
			if c < '0' || c > '9' {
				n = -1
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 {
			return n
		}
	}
	t.Fatalf("no gutter number in view:\n%s", stripANSI(view))
	return 0
}

// Entering Visual mode must neither move the view nor drop the syntax
// highlighting: the hand-rolled Visual renderer anchors to the textarea's
// viewport and runs the same highlighter over the selection styling.
func TestVisualModeKeepsViewAndHighlighting(t *testing.T) {
	withANSI(t)
	var lines []string
	for i := 0; i < 39; i++ {
		lines = append(lines, fmt.Sprintf("line %02d with text", i))
	}
	lines = append(lines, "# Final Heading")
	m := New(strings.Join(lines, "\n"), true, nil)
	m.SetLanguage("markdown")
	m.SetSize(60, 10)
	m.Focus()
	_ = m.View()

	press := func(s string) {
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		m = tm.(Model)
		_ = m.View()
	}

	press("G") // scroll to the bottom (cursor on the heading)
	before := firstGutterNum(t, m.View())

	press("v") // enter Visual
	view := m.View()
	after := firstGutterNum(t, view)
	if before != after {
		t.Fatalf("entering Visual moved the view: first line %d -> %d", before, after)
	}
	// The heading keeps its style under the selection.
	if !strings.Contains(view, "\x1b[1;38;5;81m") {
		t.Fatalf("Visual mode dropped markdown highlighting:\n%q", view)
	}
	// And the selection/cursor styling is present too.
	sel := visualCursorStyle.Render("Z")
	if open := sel[:strings.Index(sel, "Z")]; !strings.Contains(view, open) {
		t.Fatalf("Visual selection styling missing (want %q):\n%q", open, view)
	}

	// Extending the selection upward past the window scrolls minimally.
	for i := 0; i < 12; i++ {
		press("k")
	}
	scrolled := firstGutterNum(t, m.View())
	if scrolled >= before {
		t.Fatalf("Visual window did not follow the cursor up: first line %d", scrolled)
	}
}

// Undo must restore the buffer with the change site centered, not pinned to
// the view's bottom edge (SetValue resets the viewport to the top).
func TestUndoKeepsViewOnChange(t *testing.T) {
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("L%02d content", i))
	}
	m := New(strings.Join(lines, "\n"), true, nil)
	m.SetSize(60, 10) // 9 buffer rows + status line
	m.Focus()
	_ = m.View()

	press := func(msg tea.KeyMsg) {
		tm, _ := m.Update(msg)
		m = tm.(Model)
		_ = m.View()
	}
	keys := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	press(keys("3"))
	press(keys("0"))
	press(keys("j")) // cursor to row 30
	if r, _ := m.cursorPos(); r != 30 {
		t.Fatalf("setup: row = %d, want 30", r)
	}
	press(keys("x")) // small edit (pushes undo)
	press(keys("u")) // undo

	if r, _ := m.cursorPos(); r != 30 {
		t.Fatalf("undo: cursor row = %d, want 30", r)
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "L30 content") {
		t.Fatalf("change line not visible after undo:\n%s", view)
	}
	// Centered: with 9 rows and the two-step jump, the window should start
	// well below the top and above the cursor (not bottom-edge pinned at 22).
	if !strings.Contains(view, "L26 content") || strings.Contains(view, "L22 content") {
		t.Fatalf("undo view not centered on the change:\n%s", view)
	}
}

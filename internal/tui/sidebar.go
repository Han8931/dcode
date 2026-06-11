package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sidebarItem is one row: a challenge and its progress in the tutor TUI, or a
// node of the vault's file tree (a directory or note) in the vault TUI.
type sidebarItem struct {
	id     string
	title  string // first line of the prompt, the topic title, or the file name
	status string // "done" | "in_progress" | ""
	active bool   // true when this row is open in the editor (rendered bold)
	header bool   // a non-selectable section heading (e.g. a curriculum module)

	// Tree fields (vault TUI). depth indents the row; dir marks a directory
	// node (▸ collapsed / ▾ expanded); marked is the space-bar selection used
	// by the NERDTree-style batch operations. root marks the synthetic vault-
	// root row at the top of the tree: a directory you can add into but never
	// move, delete, or mark.
	depth    int
	dir      bool
	expanded bool
	marked   bool
	root     bool
}

// sidebarModel is the left pane: a hand-rolled cursor list. Hand-rolled rather
// than bubbles/list so it doesn't fight the parent for j/k and filter keys.
type sidebarModel struct {
	items   []sidebarItem
	cursor  int
	w, h    int
	focused bool
}

func newSidebar() sidebarModel { return sidebarModel{} }

func (s *sidebarModel) setSize(w, h int) { s.w, s.h = w, h }

// setItems replaces the list, keeping the cursor on the same id when possible
// (so a status refresh doesn't jump the selection). The cursor always lands on a
// selectable (non-header) row.
func (s *sidebarModel) setItems(items []sidebarItem) {
	var keepID string
	if s.cursor >= 0 && s.cursor < len(s.items) {
		keepID = s.items[s.cursor].id
	}
	s.items = items
	s.cursor = s.firstSelectable()
	for i, it := range items {
		if !it.header && it.id == keepID {
			s.cursor = i
			break
		}
	}
}

func (s sidebarModel) firstSelectable() int {
	for i, it := range s.items {
		if !it.header {
			return i
		}
	}
	return 0
}

// selected returns the highlighted item, or ok=false when there is no selectable
// item under the cursor.
func (s sidebarModel) selected() (sidebarItem, bool) {
	if s.cursor < 0 || s.cursor >= len(s.items) || s.items[s.cursor].header {
		return sidebarItem{}, false
	}
	return s.items[s.cursor], true
}

// Update handles cursor movement when focused, skipping header rows. Returns
// whether Enter was pressed so the parent can act on the selection.
func (s sidebarModel) Update(msg tea.Msg) (sidebarModel, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, false
	}
	switch key.String() {
	case "j", "down":
		s.move(1)
	case "k", "up":
		s.move(-1)
	case "g", "home":
		s.cursor = s.firstSelectable()
	case "G", "end":
		s.cursor = s.lastSelectable()
	case "enter":
		return s, true
	}
	return s, false
}

// move steps the cursor by dir (±1), skipping headers and stopping at the ends.
func (s *sidebarModel) move(dir int) {
	for i := s.cursor + dir; i >= 0 && i < len(s.items); i += dir {
		if !s.items[i].header {
			s.cursor = i
			return
		}
	}
}

func (s sidebarModel) lastSelectable() int {
	for i := len(s.items) - 1; i >= 0; i-- {
		if !s.items[i].header {
			return i
		}
	}
	return 0
}

func (s sidebarModel) view() string {
	if len(s.items) == 0 {
		return hintStyle.Render("(nothing here yet)")
	}

	// Show a window of items that fits the pane height, scrolled to keep the
	// cursor visible. Returning more lines than s.h would overflow the box.
	lo, hi := s.window()

	var b strings.Builder
	for i := lo; i < hi; i++ {
		it := s.items[i]
		row := s.renderRow(it, i == s.cursor)
		b.WriteString(row)
		if i < hi-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// window returns the [lo, hi) slice of items to display so the cursor stays in
// view within s.h rows.
func (s sidebarModel) window() (int, int) {
	h := s.h
	if h <= 0 || h >= len(s.items) {
		return 0, len(s.items)
	}
	lo := s.cursor - h/2
	if lo < 0 {
		lo = 0
	}
	hi := lo + h
	if hi > len(s.items) {
		hi = len(s.items)
		lo = hi - h
	}
	return lo, hi
}

// rowGlyph picks the one-cell marker before the title: the fold state for
// directories, the progress state for challenges, a blank otherwise.
func (it sidebarItem) rowGlyph() string {
	switch {
	case it.dir && it.expanded:
		return "▾"
	case it.dir:
		return "▸"
	case it.status == "done":
		return "✓"
	case it.status == "in_progress":
		return "…"
	}
	return " "
}

func (s sidebarModel) renderRow(it sidebarItem, selected bool) string {
	if it.header {
		return headerRow.Width(s.w).Render(it.title)
	}

	indent := strings.Repeat("  ", it.depth)
	glyph := it.rowGlyph()

	title := it.title
	max := s.w - len(indent) - 2 // reserve room for the glyph and a space
	if max > 0 && lipgloss.Width(title) > max {
		title = truncate(title, max)
	}

	if !selected {
		g := glyph
		switch it.status {
		case "done":
			g = doneGlyph.Render(glyph)
		case "in_progress":
			g = wipGlyph.Render(glyph)
		}
		t := title
		switch {
		case it.root:
			t = activeItem.Render(title) // the vault root reads as the container
		case it.marked:
			t = markedItem.Render(title) // space-marked for a batch op
		case it.active:
			t = activeItem.Render(title) // open in the editor
		}
		return indent + g + " " + t
	}

	// Paint one continuous cursor bar across the whole row. We style each segment
	// with the bar background rather than wrapping the finished row in a single
	// style: a nested glyph color emits an SGR reset that would otherwise punch a
	// hole in the highlight after it. The bar is bright when the pane is focused
	// and dimmed when it's blurred, like ranger/lf fade an inactive pane.
	bg := selectedBg
	if !s.focused {
		bg = selectedBlurredBg
	}
	base := lipgloss.NewStyle().Background(bg).Foreground(selectedFg)

	g := base.Render(glyph)
	switch it.status {
	case "done":
		g = base.Foreground(doneColor).Render(glyph)
	case "in_progress":
		g = base.Foreground(wipColor).Render(glyph)
	}
	t := base.Render(" " + title)
	switch {
	case it.root:
		t = base.Bold(true).Render(" " + title)
	case it.marked:
		t = base.Foreground(wipColor).Render(" " + title)
	case it.active:
		t = base.Bold(true).Render(" " + title)
	}

	row := base.Render(indent) + g + t
	if pad := s.w - len(indent) - 2 - lipgloss.Width(title); pad > 0 {
		row += base.Render(strings.Repeat(" ", pad))
	}
	return row
}

// truncate cuts s to at most w display cells, adding an ellipsis.
func truncate(s string, w int) string {
	if w <= 1 {
		return "…"
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}

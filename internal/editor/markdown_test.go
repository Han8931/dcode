package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParagraphMotion(t *testing.T) {
	m := New("a1\na2\n\nb1\nb2\n\nc1\n", true, nil)
	m.SetSize(60, 12)

	press := func(k string) {
		var tm tea.Model
		tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = tm.(Model)
	}
	row := func() int { r, _ := m.cursorPos(); return r }

	press("}")
	if row() != 2 {
		t.Fatalf("} from top: row = %d, want 2 (first blank line)", row())
	}
	press("}")
	if row() != 5 {
		t.Fatalf("}} : row = %d, want 5 (second blank line)", row())
	}
	press("}")
	if row() != 7 {
		t.Fatalf("}}} clamps to the last line, row = %d, want 7", row())
	}
	press("{")
	press("{")
	if row() != 2 {
		t.Fatalf("{{ back: row = %d, want 2", row())
	}
	press("{")
	if row() != 0 {
		t.Fatalf("{ clamps to the top, row = %d, want 0", row())
	}

	// Counts compose: 2} crosses two paragraphs.
	press("2")
	press("}")
	if row() != 5 {
		t.Fatalf("2} : row = %d, want 5", row())
	}
}

func TestHighlightMarkdown(t *testing.T) {
	withANSI(t)
	src := "# Title\n" +
		"text with `x := 1` and a [[Other Note]] link\n" +
		"> quoted wisdom\n" +
		"```go\n" +
		"func main() {}\n" +
		"```\n" +
		"plain tail for and import words\n"
	out := highlightMarkdown(src, false, nil)
	rows := strings.Split(out, "\n")

	checks := []struct {
		row  int
		want string
		desc string
	}{
		{0, "\x1b[1;38;5;81m# Title", "heading styled bold cyan"},
		{1, "\x1b[38;5;222m`x := 1`", "inline code span styled"},
		{1, "\x1b[38;5;79m[[Other Note]]", "wikilink styled"},
		{2, "\x1b[3;38;5;244m> quoted wisdom", "blockquote styled"},
		{3, "\x1b[38;5;222m```go", "fence delimiter styled"},
		{4, "\x1b[1;38;5;81mfunc\x1b[0m", "go keyword highlighted inside fence"},
	}
	for _, c := range checks {
		if !strings.Contains(rows[c.row], c.want) {
			t.Errorf("%s: row %d = %q", c.desc, c.row, rows[c.row])
		}
	}
	// Prose outside any construct is untouched — no python-keyword bleed.
	if rows[6] != "plain tail for and import words" {
		t.Errorf("plain prose altered: %q", rows[6])
	}
	// Highlighting only recolors: the visible text must be byte-identical.
	// (Regression: styling through a newline made lipgloss pad rows, shifting
	// the next line sideways.)
	if got := stripANSI(out); got != src {
		t.Errorf("visible text changed by highlighting:\n got %q\nwant %q", got, src)
	}
	if code := "x = 1  # note\nnext line\n"; stripANSI(highlightCode(code, pythonSyntax())) != code {
		t.Errorf("code highlighting changed visible text: %q", stripANSI(highlightCode(code, pythonSyntax())))
	}
	// Heading detection skips the line-number gutter the textarea prepends —
	// but only when the gutter is actually drawn: with it off, a line that
	// genuinely starts with digits must not lose them to the strip.
	gut := highlightMarkdownRow("  1 # Gutter Heading", &mdState{gutter: true})
	if !strings.Contains(gut, "  1 \x1b[1;38;5;81m# Gutter Heading") {
		t.Errorf("gutter heading = %q", gut)
	}
	if row := highlightMarkdownRow("1 # not a heading", &mdState{}); strings.Contains(row, "\x1b[1;38;5;81m") {
		t.Errorf("digit-leading text misread as gutter+heading: %q", row)
	}
}

func TestHighlightMarkdownEmphasisAndLists(t *testing.T) {
	withANSI(t)
	src := "some *italic* and **bold** and ***both*** and ****strong**** text\n" +
		"2 * 3 stays * plain\n" +
		"- item one\n" +
		"  * nested with **bold** inside\n" +
		"1. first step\n" +
		"12) twelfth step\n" +
		"3.14 is pi, not a list\n"
	out := highlightMarkdown(src, false, nil)
	rows := strings.Split(out, "\n")

	for _, c := range []struct {
		row  int
		want string
		desc string
	}{
		{0, "\x1b[3;38;5;216m*italic*\x1b[0m", "italic span colored peach"},
		{0, "\x1b[1;38;5;222m**bold**\x1b[0m", "bold span colored gold"},
		{0, "\x1b[1;3;38;5;219m***both***\x1b[0m", "bold-italic span colored pink"},
		{0, "\x1b[1;38;5;213m****strong****\x1b[0m", "four-star strong span colored magenta"},
		{2, "\x1b[38;5;75m-\x1b[0m item one", "dash bullet tinted"},
		{3, "\x1b[38;5;75m*\x1b[0m nested", "star bullet tinted"},
		{3, "\x1b[1;38;5;222m**bold**\x1b[0m inside", "inline styling inside a list item"},
		{4, "\x1b[38;5;75m1.\x1b[0m first step", "numbered marker tinted"},
		{5, "\x1b[38;5;75m12)\x1b[0m twelfth step", "paren marker tinted"},
	} {
		if !strings.Contains(rows[c.row], c.want) {
			t.Errorf("%s: row %d = %q", c.desc, c.row, rows[c.row])
		}
	}
	// Flanking rules: "2 * 3" has space-touching stars — no emphasis.
	if rows[1] != "2 * 3 stays * plain" {
		t.Errorf("loose stars styled: %q", rows[1])
	}
	// A number that isn't followed by "<digits>. " stays prose.
	if rows[6] != "3.14 is pi, not a list" {
		t.Errorf("decimal misread as list marker: %q", rows[6])
	}
	if got := stripANSI(out); got != src {
		t.Errorf("visible text changed:\n got %q\nwant %q", got, src)
	}
}

func TestHighlightMarkdownLinksAndRules(t *testing.T) {
	withANSI(t)
	src := "see [the docs](https://example.com) for more\n" +
		"---\n" +
		"checkbox [x] and lone [brackets] stay plain\n"
	out := highlightMarkdown(src, false, nil)
	rows := strings.Split(out, "\n")

	if !strings.Contains(rows[0], "\x1b[38;5;79m[the docs]\x1b[0m") {
		t.Errorf("link text not styled: %q", rows[0])
	}
	if !strings.Contains(rows[0], "\x1b[38;5;244m(https://example.com)\x1b[0m") {
		t.Errorf("link url not dimmed: %q", rows[0])
	}
	if !strings.Contains(rows[1], "\x1b[38;5;240m---") {
		t.Errorf("thematic break not styled: %q", rows[1])
	}
	if rows[2] != "checkbox [x] and lone [brackets] stay plain" {
		t.Errorf("plain brackets styled: %q", rows[2])
	}
	if got := stripANSI(out); got != src {
		t.Errorf("visible text changed:\n got %q\nwant %q", got, src)
	}
}

// Styling must not flicker as the cursor moves: the cursor's escape sequences
// can split an inline delimiter, and matching has to see through them.
func TestHighlightMarkdownCursorOnDelimiter(t *testing.T) {
	withANSI(t)
	cursor := "\x1b[7;38;5;42m" // the editor's block-cursor styling

	// Cursor on the second star of "**bold**".
	row := "x *" + cursor + "*\x1b[0m" + "bold** y"
	out := mdInline(row)
	if !strings.Contains(out, "\x1b[1;38;5;222m") {
		t.Errorf("split ** delimiter lost bold styling: %q", out)
	}
	// Cursor on the second bracket of a wikilink.
	row = "see [" + cursor + "[\x1b[0m" + "Note]] now"
	out = mdInline(row)
	if !strings.Contains(out, "\x1b[38;5;79m") {
		t.Errorf("split [[ delimiter lost link styling: %q", out)
	}
	// Cursor on an inline-code backtick.
	row = "a " + cursor + "`\x1b[0m" + "x := 1` b"
	out = mdInline(row)
	if !strings.Contains(out, "\x1b[38;5;222m") {
		t.Errorf("split backtick lost code styling: %q", out)
	}
}

// Block styling must not depend on the scroll position: a fence whose opener
// is above the viewport still styles its visible body, via the buffer scan
// re-synced through the rows' line numbers.
func TestHighlightMarkdownScrolledFence(t *testing.T) {
	withANSI(t)
	buffer := "# Title\n```go\nfunc a() {}\nfunc b() {}\n```\nplain for text"
	states := mdScanBuffer(buffer)

	// The viewport shows only rows 3-6: the ```go opener is off-screen.
	view := "  3 func a() {}\n  4 func b() {}\n  5 ```\n  6 plain for text"
	rows := strings.Split(highlightMarkdown(view, true, states), "\n")

	if !strings.Contains(rows[0], "\x1b[1;38;5;81mfunc\x1b[0m") {
		t.Errorf("scrolled fence body lost go highlighting: %q", rows[0])
	}
	if !strings.Contains(rows[2], "\x1b[38;5;222m```") {
		t.Errorf("closing fence not code-colored: %q", rows[2])
	}
	if !strings.Contains(rows[3], "plain for text") || strings.Contains(rows[3], "\x1b[1;38;5;81m") {
		t.Errorf("row after the fence should be plain: %q", rows[3])
	}
}

// A blockquote spans rows: soft-wrapped/continuation rows without their own
// ">" stay quoted until a blank row; headings and bullets interrupt it.
func TestHighlightMarkdownQuoteSpansRows(t *testing.T) {
	withANSI(t)
	src := "> quoted first row\n" +
		"wrapped continuation row\n" +
		"\n" +
		"plain after blank\n" +
		"> another quote\n" +
		"- list interrupts\n"
	out := highlightMarkdown(src, false, nil)
	rows := strings.Split(out, "\n")

	quote := "\x1b[3;38;5;244m"
	if !strings.Contains(rows[0], quote+"> quoted first row") {
		t.Errorf("quote row unstyled: %q", rows[0])
	}
	if !strings.Contains(rows[1], quote+"wrapped continuation row") {
		t.Errorf("continuation row should stay quoted: %q", rows[1])
	}
	if rows[3] != "plain after blank" {
		t.Errorf("blank row should end the quote: %q", rows[3])
	}
	if !strings.Contains(rows[5], "\x1b[38;5;75m-\x1b[0m list interrupts") {
		t.Errorf("a list item should interrupt the quote: %q", rows[5])
	}
	if got := stripANSI(out); got != src {
		t.Errorf("visible text changed:\n got %q\nwant %q", got, src)
	}
}

// A long jump ({, }) must scroll the viewport so the cursor stays visible.
func TestJumpKeepsCursorInView(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		if i%5 == 4 {
			lines = append(lines, "")
		} else {
			lines = append(lines, "line content")
		}
	}
	lines = append(lines, "BOTTOM marker line")
	m := New(strings.Join(lines, "\n"), true, nil)
	m.SetSize(60, 8)
	m.Focus()
	_ = m.View() // a frame must render before the viewport can scroll

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = tm.(Model)
	if !strings.Contains(m.View(), "BOTTOM marker line") {
		t.Fatal("G should scroll the view to the cursor")
	}
	// { jumps a paragraph back up — view must follow the cursor again.
	for i := 0; i < 9; i++ {
		tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("{")})
		m = tm.(Model)
		_ = m.View() // render a frame, like the app between keys
	}
	row, _ := m.cursorPos()
	if row > 8 {
		t.Fatalf("{ x9 should be near the top, row = %d", row)
	}
	if !strings.Contains(stripANSI(m.View()), "  1 ") {
		t.Fatalf("view did not scroll up with the cursor:\n%s", stripANSI(m.View()))
	}
}

func TestJumplist(t *testing.T) {
	m := New("a1\na2\n\nb1\n\nc1\nc2\nc3", true, nil)
	m.SetSize(60, 12)
	m.Focus()

	press := func(msg tea.KeyMsg) {
		tm, _ := m.Update(msg)
		m = tm.(Model)
	}
	keys := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	row := func() int { r, _ := m.cursorPos(); return r }

	press(keys("G")) // jump to bottom, recording row 0
	if row() != 7 {
		t.Fatalf("G: row = %d, want 7", row())
	}
	press(keys("{")) // paragraph jump, recording row 7
	if row() != 4 {
		t.Fatalf("{: row = %d, want 4", row())
	}
	press(tea.KeyMsg{Type: tea.KeyCtrlO}) // back to where { left
	if row() != 7 {
		t.Fatalf("ctrl+o: row = %d, want 7", row())
	}
	press(tea.KeyMsg{Type: tea.KeyCtrlO}) // back to where G left
	if row() != 0 {
		t.Fatalf("ctrl+o x2: row = %d, want 0", row())
	}
	press(tea.KeyMsg{Type: tea.KeyCtrlO}) // at the oldest: stays put
	if row() != 0 {
		t.Fatalf("ctrl+o at oldest moved: row = %d", row())
	}
	press(tea.KeyMsg{Type: tea.KeyTab}) // forward again (Ctrl-I)
	if row() != 7 {
		t.Fatalf("tab: row = %d, want 7", row())
	}
	press(tea.KeyMsg{Type: tea.KeyTab}) // and to the stashed live position
	if row() != 4 {
		t.Fatalf("tab x2: row = %d, want 4", row())
	}
	// A new jump truncates the forward history.
	press(tea.KeyMsg{Type: tea.KeyCtrlO})
	press(keys("G"))
	if row() != 7 {
		t.Fatalf("G after walking back: row = %d, want 7", row())
	}
	press(tea.KeyMsg{Type: tea.KeyTab}) // nothing forward anymore
	if row() != 7 {
		t.Fatalf("tab after truncation moved: row = %d", row())
	}
}

func TestCtrlEYScroll(t *testing.T) {
	m := New("a\nb\nc\nd\ne\n", true, nil)
	m.SetSize(60, 4)
	m.Focus()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = tm.(Model)
	if row, _ := m.cursorPos(); row != 1 {
		t.Fatalf("ctrl+e: row = %d, want 1", row)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	m = tm.(Model)
	if row, _ := m.cursorPos(); row != 0 {
		t.Fatalf("ctrl+y: row = %d, want 0", row)
	}
}

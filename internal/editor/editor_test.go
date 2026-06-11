package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func forceColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func apply(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		tm, _ := m.Update(msg)
		m = tm.(Model)
	}
	return m
}

func TestVimModeSwitchingAndInsert(t *testing.T) {
	m := New("", true, nil)
	m.SetSize(40, 10)
	if m.mode != modeNormal {
		t.Fatalf("vim editor should open in Normal mode")
	}

	// i enters Insert; typing lands in the buffer; Esc returns to Normal.
	m = apply(m, key("i"), key("h"), key("e"), key("y"))
	if m.mode != modeInsert {
		t.Fatalf("expected Insert mode after 'i'")
	}
	if got := m.Value(); got != "hey" {
		t.Fatalf("buffer = %q, want %q", got, "hey")
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeNormal {
		t.Fatalf("Esc should return to Normal mode")
	}
}

func TestVimDeleteLineAndOpenBelow(t *testing.T) {
	m := New("alpha\nbeta\ngamma", true, nil)
	m.SetSize(40, 10)

	// Cursor starts at top (line 0). dd removes the first line.
	m = apply(m, key("d"), key("d"))
	if got := m.Value(); got != "beta\ngamma" {
		t.Fatalf("after dd buffer = %q, want %q", got, "beta\ngamma")
	}

	// o opens a line below the current one and enters Insert.
	m = apply(m, key("o"), key("X"))
	if m.mode != modeInsert {
		t.Fatalf("o should enter Insert mode")
	}
	if got := m.Value(); got != "beta\nX\ngamma" {
		t.Fatalf("after o X buffer = %q, want %q", got, "beta\nX\ngamma")
	}
}

func TestVimReplaceChar(t *testing.T) {
	m := New("cat", true, nil)
	m.SetSize(40, 10)
	// Cursor on 'c'; r b replaces it with 'b'.
	m = apply(m, key("r"), key("b"))
	if got := m.Value(); got != "bat" {
		t.Fatalf("after rb buffer = %q, want %q", got, "bat")
	}
	if m.mode != modeNormal {
		t.Fatalf("replace should stay in Normal mode")
	}
}

func TestStatusBadgeReflectsMode(t *testing.T) {
	m := New("", true, nil)
	m.SetSize(40, 10)
	if !strings.Contains(m.statusLine(), "NORMAL") {
		t.Errorf("status line should show NORMAL in normal mode: %q", m.statusLine())
	}
	m = apply(m, key("i"))
	if !strings.Contains(m.statusLine(), "INSERT") {
		t.Errorf("status line should show INSERT after 'i': %q", m.statusLine())
	}
}

func TestViewHighlightsGoKeywordsAndTypes(t *testing.T) {
	forceColor(t)
	view := highlightSyntax("go", "func greet(name string) string {\n\treturn name\n}")

	if !strings.Contains(view, keywordStyle.Render("func")) {
		t.Fatal("view should style the func keyword")
	}
	if !strings.Contains(view, typeStyle.Render("string")) {
		t.Fatal("view should style the string type")
	}
	if !strings.Contains(view, keywordStyle.Render("return")) {
		t.Fatal("view should style the return keyword")
	}
}

func TestSyntaxHighlightingSkipsStringsAndComments(t *testing.T) {
	forceColor(t)
	got := highlightSyntax("go", "func x() string {\n\t// return string\n\treturn \"func string\"\n}\n")
	if strings.Count(got, keywordStyle.Render("return")) != 1 {
		t.Fatalf("only the real return keyword should be keyword-styled:\n%s", got)
	}
	if strings.Contains(got, keywordStyle.Render("func string")) {
		t.Fatalf("keywords inside strings should not be keyword-styled:\n%s", got)
	}
	if !strings.Contains(got, commentStyle.Render("// return string")) {
		t.Fatalf("line comment should be comment-styled:\n%s", got)
	}
	if !strings.Contains(got, stringStyle.Render("\"func string\"")) {
		t.Fatalf("string literal should be string-styled:\n%s", got)
	}
}

func TestSyntaxHighlightingSupportsPython(t *testing.T) {
	forceColor(t)
	got := highlightSyntax("python", "def greet(name: str):\n    # return string\n    return \"hi\"\n")
	if !strings.Contains(got, keywordStyle.Render("def")) {
		t.Fatalf("Python def should be keyword-styled:\n%s", got)
	}
	if !strings.Contains(got, typeStyle.Render("str")) {
		t.Fatalf("Python str should be type-styled:\n%s", got)
	}
	if !strings.Contains(got, commentStyle.Render("# return string")) {
		t.Fatalf("Python comment should be comment-styled:\n%s", got)
	}
	if !strings.Contains(got, stringStyle.Render("\"hi\"")) {
		t.Fatalf("Python string should be string-styled:\n%s", got)
	}
}

func TestSyntaxHighlightingLeavesPhysicsPlain(t *testing.T) {
	text := "Force equals mass times acceleration.\nreturn is just a word here."
	if got := highlightSyntax("physics", text); got != text {
		t.Fatalf("physics prose should not be syntax-highlighted:\n%q", got)
	}
}

func TestSyntaxHighlightingHandlesCursorSplitTokens(t *testing.T) {
	forceColor(t)
	got := highlightSyntax("go", "\x1b[7mf\x1b[0munc main() {\n\treturn\n}\n")
	if !strings.Contains(got, keywordStyle.Render("unc")) {
		t.Fatalf("keyword split by cursor ANSI should still be styled:\n%q", got)
	}
}

func TestPendingOperatorClearedByEsc(t *testing.T) {
	m := New("hello", true, nil)
	m.SetSize(40, 10)
	m = apply(m, key("d")) // arm operator
	if m.pending == 0 {
		t.Fatal("'d' should arm a pending operator")
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyEsc}) // cancel
	if m.pending != 0 {
		t.Fatal("Esc should cancel the pending operator")
	}
	if got := m.Value(); got != "hello" {
		t.Fatalf("buffer should be unchanged after d<Esc>, got %q", got)
	}
}

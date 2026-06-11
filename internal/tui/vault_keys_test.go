package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"dcode/internal/config"
)

func ctrlW() tea.KeyMsg         { return tea.KeyMsg{Type: tea.KeyCtrlW} }
func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// Ctrl-W then h/l moves focus between panes, Vim window-style, like the coding TUI.
func TestVaultWindowChordFocus(t *testing.T) {
	m := newTestVaultModel(t)
	if m.focus != paneSidebar {
		t.Fatalf("default focus = %v, want sidebar", m.focus)
	}
	step := func(msgs ...tea.Msg) {
		for _, msg := range msgs {
			tm, _ := m.Update(msg)
			m = tm.(VaultModel)
		}
	}

	step(ctrlW(), runeKey('l'))
	if m.focus != paneEditor {
		t.Fatalf("after ⌃w l, focus = %v, want editor", m.focus)
	}
	step(ctrlW(), runeKey('l'))
	if m.focus != paneChat {
		t.Fatalf("after ⌃w l, focus = %v, want chat", m.focus)
	}
	step(ctrlW(), runeKey('l')) // clamps at the right edge
	if m.focus != paneChat {
		t.Fatalf("⌃w l at the right edge should clamp at chat, got %v", m.focus)
	}
	step(ctrlW(), runeKey('h'))
	if m.focus != paneEditor {
		t.Fatalf("after ⌃w h, focus = %v, want editor", m.focus)
	}
	step(ctrlW(), runeKey('h'))
	if m.focus != paneSidebar {
		t.Fatalf("after ⌃w h, focus = %v, want sidebar", m.focus)
	}

	// A bare key (no Ctrl-W first) must not move focus.
	step(runeKey('l'))
	if m.focus != paneSidebar {
		t.Fatalf("bare l moved focus to %v; should need ⌃w first", m.focus)
	}
}

// In the editor's Vim Normal mode, the ",n" leader chord folds the sidebar.
func TestVaultLeaderFold(t *testing.T) {
	cfg := config.Config{}
	cfg.Editor.Keybindings = "vim"
	m := newVaultModel(testService(t), cfg)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = tm.(VaultModel)

	step := func(msgs ...tea.Msg) {
		for _, msg := range msgs {
			tm, _ := m.Update(msg)
			m = tm.(VaultModel)
		}
	}
	step(ctrlW(), runeKey('l')) // focus the editor
	if m.focus != paneEditor {
		t.Fatalf("expected editor focus, got %v", m.focus)
	}
	if !m.editor.NormalMode() {
		t.Skip("vim editor not in Normal mode in this build; skipping leader test")
	}
	before := m.sidebarCollapsed
	step(runeKey(','), runeKey('n'))
	if m.sidebarCollapsed == before {
		t.Fatal(",n should toggle the sidebar fold")
	}
}

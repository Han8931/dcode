package editor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestJumpToLineRecordsOrigin(t *testing.T) {
	m := New("l1\nl2\nl3\nl4\nl5", true, nil)
	m.SetSize(60, 10)

	m.JumpToLine(4) // records the origin (line 1) then moves
	if m.CursorLine() != 4 {
		t.Fatalf("after JumpToLine(4), CursorLine = %d, want 4", m.CursorLine())
	}
	// Ctrl-O returns to the origin in-file — no chaining to the parent.
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = tm.(Model)
	if cmd != nil {
		t.Fatal("Ctrl-O with in-file history should move, not chain to the parent")
	}
	if m.CursorLine() != 1 {
		t.Fatalf("after Ctrl-O, CursorLine = %d, want 1", m.CursorLine())
	}
}

func TestCtrlOChainsWhenInFileHistoryExhausted(t *testing.T) {
	m := New("a\nb\nc", true, nil)
	m.SetSize(60, 10)

	// Empty jumplist: Ctrl-O should hand off to the parent's cross-file history.
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("Ctrl-O with an empty jumplist should chain to the parent")
	}
	if rc, ok := cmd().(RunCommandMsg); !ok || rc.Raw != "jumpback" {
		t.Fatalf("Ctrl-O should forward jumpback, got %#v", cmd())
	}
	// Tab (Ctrl-I) chains forward the same way.
	tm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = tm.(Model)
	if rc, ok := cmd().(RunCommandMsg); !ok || rc.Raw != "jumpforward" {
		t.Fatalf("Tab should forward jumpforward, got %#v", cmd())
	}
}

func TestGtGTForwardTabCommands(t *testing.T) {
	m := New("x", true, nil)
	m.SetSize(60, 10)

	m = apply(m, key("g"))
	tm, cmd := m.Update(key("t"))
	m = tm.(Model)
	if rc, ok := cmd().(RunCommandMsg); !ok || rc.Raw != "tabnext" {
		t.Fatalf("gt should forward tabnext, got %#v", cmd())
	}
	m = apply(m, key("g"))
	tm, cmd = m.Update(key("T"))
	m = tm.(Model)
	if rc, ok := cmd().(RunCommandMsg); !ok || rc.Raw != "tabprev" {
		t.Fatalf("gT should forward tabprev, got %#v", cmd())
	}
}

func TestHLSwitchTabs(t *testing.T) {
	m := New("x", true, nil)
	m.SetSize(60, 10)

	_, cmd := m.Update(key("L"))
	if rc, ok := cmd().(RunCommandMsg); !ok || rc.Raw != "tabnext" {
		t.Fatalf("L should forward tabnext, got %#v", cmd())
	}
	_, cmd = m.Update(key("H"))
	if rc, ok := cmd().(RunCommandMsg); !ok || rc.Raw != "tabprev" {
		t.Fatalf("H should forward tabprev, got %#v", cmd())
	}
}

func TestResetJumpsClearsInFileHistory(t *testing.T) {
	m := New("l1\nl2\nl3", true, nil)
	m.SetSize(60, 10)
	m.JumpToLine(3)
	m.ResetJumps()
	// With the list cleared, Ctrl-O has nothing in-file and chains to the parent.
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	_ = tm
	if cmd == nil {
		t.Fatal("after ResetJumps, Ctrl-O should chain (in-file jumplist empty)")
	}
}

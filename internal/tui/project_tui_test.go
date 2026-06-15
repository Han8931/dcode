package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"dcode/internal/config"
)

// modelWithNotesBase builds a vault model whose notes base is a temp dir, and
// redirects the global recents file into a temp config dir so the test never
// touches the real one.
func modelWithNotesBase(t *testing.T) VaultModel {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := config.Config{}
	cfg.NotesDir = t.TempDir()
	m := newVaultModel(testService(t), cfg)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return tm.(VaultModel)
}

func TestVaultOpenProjectSwitchesLive(t *testing.T) {
	m := modelWithNotesBase(t)

	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tm, cmd := m.runEx("open " + proj)
	m = tm.(VaultModel)
	if cmd == nil {
		t.Fatal("expected a load command after :open")
	}
	if got := m.svc.ProjectName(); got != filepath.Base(proj) {
		t.Fatalf("project not switched: ProjectName = %q, want %q", got, filepath.Base(proj))
	}
	if m.current != "" {
		t.Fatalf("open file should be reset on project switch, got %q", m.current)
	}
	if m.focus != paneSidebar {
		t.Fatal("switching project should focus the sidebar")
	}
	// The opened project is remembered for the picker.
	if rs := config.LoadRecents(); len(rs) == 0 || filepath.Base(rs[0]) != filepath.Base(proj) {
		t.Fatalf("opened project not recorded in recents: %v", rs)
	}
}

func TestVaultOpenProjectBadPathKeepsCurrent(t *testing.T) {
	m := modelWithNotesBase(t)
	before := m.svc
	tm, _ := m.runEx("open " + filepath.Join(t.TempDir(), "does-not-exist"))
	m = tm.(VaultModel)
	if m.svc != before {
		t.Fatal("a bad path must not switch the project")
	}
}

func TestVaultOpenPickerOpensModal(t *testing.T) {
	m := modelWithNotesBase(t)
	// ",o" from the sidebar opens the picker modal.
	tm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	m = tm.(VaultModel)
	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = tm.(VaultModel)
	if !m.pickerMode {
		t.Fatal(",o should open the project picker")
	}
}

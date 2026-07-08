package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"dcode/internal/config"
	"dcode/internal/core"
	"dcode/internal/tutor"
	"dcode/internal/vault"
)

func tabsModel(t *testing.T) VaultModel {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name),
			[]byte("package main\n\nfunc "+strings.TrimSuffix(name, ".go")+"() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := core.New(code, notes, tutor.New(config.AIConfig{Provider: "openai"}))
	m := newVaultModel(svc, config.Config{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return tm.(VaultModel)
}

// openFile drives the real open path (vOpenCmd → vOpenedMsg), so tab bookkeeping
// runs exactly as it does in the app.
func openFile(t *testing.T, m VaultModel, path string) VaultModel {
	t.Helper()
	tm, _ := m.Update(vOpenCmd(m.svc, path)())
	return tm.(VaultModel)
}

func TestTabsOpenReplaceAndNewTab(t *testing.T) {
	m := tabsModel(t)

	// First open creates the one tab.
	m = openFile(t, m, "a.go")
	if len(m.tabs) != 1 || m.tabs[0].path != "a.go" || m.activeTab != 0 {
		t.Fatalf("first open should create tab a.go, got %+v (active %d)", m.tabs, m.activeTab)
	}

	// An ordinary open replaces the active tab (NERDTree "o").
	m = openFile(t, m, "b.go")
	if len(m.tabs) != 1 || m.tabs[0].path != "b.go" {
		t.Fatalf("in-place open should replace the tab, got %+v", m.tabs)
	}

	// T opens a NEW tab and activates it.
	m.openNewTab = true
	m = openFile(t, m, "c.go")
	if len(m.tabs) != 2 || m.tabs[1].path != "c.go" || m.activeTab != 1 {
		t.Fatalf("T should append+activate a new tab, got %+v (active %d)", m.tabs, m.activeTab)
	}

	// Opening an already-open file activates its tab rather than duplicating.
	m = openFile(t, m, "b.go")
	if len(m.tabs) != 2 || m.activeTab != 0 {
		t.Fatalf("opening an open file should activate its tab, got %+v (active %d)", m.tabs, m.activeTab)
	}
}

func TestTabSwitchWraps(t *testing.T) {
	m := tabsModel(t)
	m = openFile(t, m, "a.go")
	m.openNewTab = true
	m = openFile(t, m, "b.go") // tabs [a,b], active 1

	tm, cmd := m.cmdTabSwitch(1) // next wraps to 0
	m = tm.(VaultModel)
	if m.activeTab != 0 || m.pendingGotoPath != "a.go" || cmd == nil {
		t.Fatalf("gt should wrap to a.go, active=%d pending=%q", m.activeTab, m.pendingGotoPath)
	}
	tm, _ = m.cmdTabSwitch(-1) // prev wraps back to 1
	m = tm.(VaultModel)
	if m.activeTab != 1 {
		t.Fatalf("gT should wrap to tab 1, active=%d", m.activeTab)
	}
}

func TestTabClose(t *testing.T) {
	m := tabsModel(t)
	m = openFile(t, m, "a.go")
	m.openNewTab = true
	m = openFile(t, m, "b.go") // [a,b] active 1

	tm, cmd := m.cmdTabClose()
	m = tm.(VaultModel)
	if len(m.tabs) != 1 || m.tabs[0].path != "a.go" || cmd == nil {
		t.Fatalf("closing active tab should leave a.go, got %+v", m.tabs)
	}
	// Closing the last tab clears the editor.
	tm, _ = m.cmdTabClose()
	m = tm.(VaultModel)
	if len(m.tabs) != 0 || m.current != "" {
		t.Fatalf("closing the last tab should clear the editor, tabs=%+v current=%q", m.tabs, m.current)
	}
}

func TestCtrlWQClosesTab(t *testing.T) {
	m := tabsModel(t)
	m = openFile(t, m, "a.go")
	m.openNewTab = true
	m = openFile(t, m, "b.go") // [a,b]

	tm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = tm.(VaultModel)
	tm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = tm.(VaultModel)
	if len(m.tabs) != 1 {
		t.Fatalf("Ctrl-W q should close a tab, got %d tabs", len(m.tabs))
	}
}

func TestQuitAllCommand(t *testing.T) {
	m := tabsModel(t)
	m = openFile(t, m, "a.go")
	_, cmd := m.runEx("qa")
	if cmd == nil {
		t.Fatal(":qa should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf(":qa should quit the app, got %T", cmd())
	}
}

func TestTabBarRendersWhenMultiple(t *testing.T) {
	m := tabsModel(t)
	m = openFile(t, m, "a.go")
	m.openNewTab = true
	m = openFile(t, m, "b.go")
	bar := m.tabBarView(80)
	if !strings.Contains(bar, "a.go") || !strings.Contains(bar, "b.go") {
		t.Fatalf("tab bar should list both files:\n%q", bar)
	}
}

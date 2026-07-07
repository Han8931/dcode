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

// :map renders the structural repo map straight into the chat with no model
// call — so, unlike :overview/:explain/:diff, it must work while offline.
func TestVaultMapWorksOffline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := core.New(code, notes, tutor.New(config.AIConfig{Provider: "openai"})) // offline
	if !svc.Offline() {
		t.Fatal("expected offline service")
	}

	m := newVaultModel(svc, config.Config{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = tm.(VaultModel)

	tm, _ = m.runEx("map")
	m = tm.(VaultModel)

	if m.streaming {
		t.Fatal(":map must not start a stream")
	}
	if strings.Contains(m.notice, "AI provider") {
		t.Fatalf(":map should work offline, but flashed a provider hint: %q", m.notice)
	}
	found := false
	for _, b := range m.chat.snapshot() {
		if strings.Contains(b.text, "func main()") {
			found = true
		}
	}
	if !found {
		t.Fatal(":map should render the repo map (with signatures) into the chat")
	}
}

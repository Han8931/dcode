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

// verifyModel builds a model over a source dir with a go.mod so a test command
// is detectable.
func verifyModel(t *testing.T) VaultModel {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
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
	svc := core.New(code, notes, tutor.New(config.AIConfig{Provider: "openai"}))
	m := newVaultModel(svc, config.Config{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return tm.(VaultModel)
}

// Bare :verify is the consent gate: it previews the detected command and never
// executes anything.
func TestVerifyConsentGate(t *testing.T) {
	m := verifyModel(t)
	tm, cmd := m.runEx("verify")
	m = tm.(VaultModel)
	if cmd != nil || m.streaming || m.verifyMode {
		t.Fatal("bare :verify must not run anything")
	}
	if !strings.Contains(m.notice, "go test ./...") || !strings.Contains(m.notice, ":verify!") {
		t.Fatalf(":verify should preview the command and point at :verify!, got %q", m.notice)
	}

	// With no detectable command, it asks the user to name one.
	m2 := newTestVaultModel(t) // empty source root: nothing to detect
	tm, _ = m2.runEx("verify")
	m2 = tm.(VaultModel)
	if !strings.Contains(m2.notice, ":verify! <command>") {
		t.Fatalf("undetectable project should ask for a command, got %q", m2.notice)
	}
}

// :verify! actually starts the run (offline is fine — the run needs no AI).
func TestVerifyBangRuns(t *testing.T) {
	m := verifyModel(t)
	tm, cmd := m.runEx("verify! echo ok")
	m = tm.(VaultModel)
	if cmd == nil || !m.streaming || !m.verifyMode {
		t.Fatalf(":verify! should start the run: streaming=%v verifyMode=%v", m.streaming, m.verifyMode)
	}

	// Completion clears the mode and does not auto-save a note.
	tm, cmd2 := m.handleStreamChunk(streamChunkMsg{done: true, full: "✓ PASSED"})
	m = tm.(VaultModel)
	if m.streaming || m.verifyMode {
		t.Fatal("completion should clear streaming/verifyMode")
	}
	if cmd2 != nil {
		t.Fatal(":verify completion should not dispatch a save")
	}
	if m.streamCancel != nil {
		t.Fatal("completion should drop the cancel func")
	}
	_ = cmd
}

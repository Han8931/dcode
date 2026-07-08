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

// gotodefModel builds a vault model over a seeded source tree with main.go open.
func gotodefModel(t *testing.T) VaultModel {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"main.go": "package main\n\nfunc run() { help() }\n",
		"util.go": "package main\n\nfunc help() {}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
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
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = tm.(VaultModel)
	m.current = "main.go" // pretend main.go is the open file
	return m
}

func TestVaultGotoDefCrossFile(t *testing.T) {
	m := gotodefModel(t)
	tm, cmd := m.runEx("def help") // help() is defined in util.go, line 3
	m = tm.(VaultModel)
	if m.pendingGotoPath != "util.go" || m.pendingGotoLine != 3 {
		t.Fatalf("cross-file jump should park util.go:3, got %q:%d",
			m.pendingGotoPath, m.pendingGotoLine)
	}
	if cmd == nil {
		t.Fatal("cross-file jump should open the defining file")
	}
	// The open resolves and the parked line is applied (cursor lands on it).
	tm, _ = m.Update(cmd())
	m = tm.(VaultModel)
	if m.current != "util.go" {
		t.Fatalf("after the jump the open file should be util.go, got %q", m.current)
	}
	if m.pendingGotoPath != "" || m.pendingGotoLine != 0 {
		t.Fatal("pending goto should be cleared after landing")
	}
}

// A cross-file gd records the origin so Ctrl-O (jumpback) returns to it, and
// Ctrl-I (jumpforward) goes back to the definition — a full round trip.
func TestVaultCrossFileJumpRoundTrip(t *testing.T) {
	m := gotodefModel(t) // main.go is open

	// gd on help() → jumps to util.go, recording main.go as the origin.
	tm, cmd := m.runEx("def help")
	m = tm.(VaultModel)
	if len(m.jumpsBack) != 1 || m.jumpsBack[0].path != "main.go" {
		t.Fatalf("cross-file jump should record main.go origin, got %+v", m.jumpsBack)
	}
	tm, _ = m.Update(cmd())
	m = tm.(VaultModel)
	if m.current != "util.go" {
		t.Fatalf("after gd the open file should be util.go, got %q", m.current)
	}

	// Ctrl-O (in-file list exhausted → jumpback) returns to main.go.
	tm, cmd = m.runEx("jumpback")
	m = tm.(VaultModel)
	if m.pendingGotoPath != "main.go" {
		t.Fatalf("jumpback should target main.go, got %q", m.pendingGotoPath)
	}
	if len(m.jumpsBack) != 0 || len(m.jumpsFwd) != 1 || m.jumpsFwd[0].path != "util.go" {
		t.Fatalf("jumpback should move util.go onto the forward stack, back=%v fwd=%v",
			m.jumpsBack, m.jumpsFwd)
	}
	tm, _ = m.Update(cmd())
	m = tm.(VaultModel)
	if m.current != "main.go" {
		t.Fatalf("Ctrl-O should return to main.go, got %q", m.current)
	}

	// Ctrl-I (jumpforward) goes back to util.go.
	tm, cmd = m.runEx("jumpforward")
	m = tm.(VaultModel)
	if m.pendingGotoPath != "util.go" {
		t.Fatalf("jumpforward should target util.go, got %q", m.pendingGotoPath)
	}
	tm, _ = m.Update(cmd())
	m = tm.(VaultModel)
	if m.current != "util.go" {
		t.Fatalf("Ctrl-I should return to util.go, got %q", m.current)
	}
}

// Jumping with an empty cross-file history reports it rather than moving.
func TestVaultJumpEmptyHistory(t *testing.T) {
	m := gotodefModel(t)
	tm, _ := m.runEx("jumpback")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "oldest") {
		t.Fatalf("jumpback with no history should flash 'oldest', got %q", m.notice)
	}
	tm, _ = m.runEx("jumpforward")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "newest") {
		t.Fatalf("jumpforward with no history should flash 'newest', got %q", m.notice)
	}
}

func TestVaultGotoDefSameFileAndMisses(t *testing.T) {
	m := gotodefModel(t)

	// run() is defined in the open file → move within it, no cross-file open.
	tm, _ := m.runEx("def run")
	m = tm.(VaultModel)
	if m.pendingGotoPath != "" {
		t.Fatal("same-file jump should not park a cross-file open")
	}
	if !strings.Contains(m.notice, "line 3") {
		t.Fatalf("same-file jump should report the line, got notice %q", m.notice)
	}

	// Unknown symbol reports a miss.
	tm, _ = m.runEx("def Nope")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "not found") {
		t.Fatalf("unknown symbol should flash 'not found', got %q", m.notice)
	}

	// Bare :def explains itself rather than jumping anywhere.
	tm, _ = m.runEx("def")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "usage") {
		t.Fatalf("bare :def should show usage, got %q", m.notice)
	}
}

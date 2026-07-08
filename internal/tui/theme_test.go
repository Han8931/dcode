package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dcode/internal/config"
	"dcode/internal/editor"
	"dcode/internal/theme"
)

// restoreTheme puts the default theme back after a test that switches it, so
// the SGR codes other tests pin (they assume Mocha) stay valid.
func restoreTheme(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		theme.Set(theme.DefaultName)
		applyThemeStyles()
		editor.ApplyTheme()
	})
}

func TestThemeCommandSwitchesLive(t *testing.T) {
	restoreTheme(t)
	m := newTestVaultModel(t)

	tm, _ := m.runEx("theme dracula")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "dracula") {
		t.Fatalf(":theme dracula should confirm, got %q", m.notice)
	}
	// The palette and the styles built from it actually switched.
	if pal.Blue != theme.Dracula.Blue {
		t.Fatalf("pal.Blue = %v, want Dracula accent", pal.Blue)
	}
	if got := focusedBorder.GetBorderTopForeground(); got != theme.Dracula.Blue {
		t.Fatalf("focused border = %v, want Dracula accent", got)
	}
}

func TestThemeCommandListsAndRejects(t *testing.T) {
	restoreTheme(t)
	m := newTestVaultModel(t)

	// Bare :theme reports the current theme and the options.
	tm, _ := m.runEx("theme")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "mocha") || !strings.Contains(m.notice, "nord") {
		t.Fatalf(":theme should list themes, got %q", m.notice)
	}

	// Unknown names are rejected with the list, and nothing changes.
	tm, _ = m.runEx("theme hotdogstand")
	m = tm.(VaultModel)
	if !strings.Contains(m.notice, "unknown theme") {
		t.Fatalf("unknown theme should be called out, got %q", m.notice)
	}
	if pal.Blue != theme.Mocha.Blue {
		t.Fatal("a rejected theme must not change the palette")
	}
}

func TestConfiguredThemeAppliesAtStartup(t *testing.T) {
	restoreTheme(t)
	svc := testService(t)
	cfg := config.Config{}
	cfg.UI.Theme = "nord"
	newVaultModel(svc, cfg)
	if pal.Blue != theme.Nord.Blue {
		t.Fatalf("startup should apply [ui] theme=nord, pal.Blue=%v", pal.Blue)
	}
}

func TestConfigCommandEnsuresFileAndLaunchesEditor(t *testing.T) {
	m := newTestVaultModel(t)

	// Without a known config path, :config explains instead of exec-ing.
	tm, cmd := m.runEx("config")
	m = tm.(VaultModel)
	if cmd != nil || !strings.Contains(m.notice, "config") {
		t.Fatalf("no-path :config should flash guidance, got %q", m.notice)
	}

	// With a path, the file is seeded from the template and an editor process
	// command is returned (the Bubble Tea runtime would exec it).
	path := filepath.Join(t.TempDir(), "config.toml")
	m.cfg.Path = path
	t.Setenv("VISUAL", "true") // a no-op binary, in case the cmd is ever run
	_, cmd = m.runEx("config")
	if cmd == nil {
		t.Fatal(":config with a path should return an exec command")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config file should have been created: %v", err)
	}
	if !strings.Contains(string(body), "[ai]") || !strings.Contains(string(body), "theme") {
		t.Fatalf("seeded config should document [ai] and theme:\n%s", body)
	}
}

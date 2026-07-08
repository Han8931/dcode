package theme

import "testing"

func TestSetSwitchesAndRestores(t *testing.T) {
	t.Cleanup(func() { Set(DefaultName) })

	if !Set("dracula") {
		t.Fatal("dracula should be a known theme")
	}
	if Current.Blue != Dracula.Blue || CurrentName() != "dracula" {
		t.Fatalf("Set(dracula): Current=%v name=%q", Current.Blue, CurrentName())
	}

	// Aliases and case-insensitivity resolve to the canonical theme.
	if !Set("TOKYO") || CurrentName() != "tokyonight" {
		t.Fatalf("alias TOKYO should select tokyonight, got %q", CurrentName())
	}

	// "" and "default" mean the built-in default.
	if !Set("") || CurrentName() != DefaultName {
		t.Fatalf("empty name should select the default, got %q", CurrentName())
	}

	// Unknown names are rejected and leave the theme unchanged.
	if Set("hotdogstand") {
		t.Fatal("unknown theme should be rejected")
	}
	if CurrentName() != DefaultName {
		t.Fatalf("failed Set must not change the theme, got %q", CurrentName())
	}
}

func TestEveryNamedThemeIsComplete(t *testing.T) {
	t.Cleanup(func() { Set(DefaultName) })
	for _, name := range Names() {
		if !Set(name) {
			t.Fatalf("Names() lists %q but Set rejects it", name)
		}
		p := Current
		for field, c := range map[string]string{
			"Base": string(p.Base), "Text": string(p.Text), "Blue": string(p.Blue),
			"Green": string(p.Green), "Red": string(p.Red), "Surface1": string(p.Surface1),
			"Overlay": string(p.Overlay), "Crust": string(p.Crust),
		} {
			if len(c) != 7 || c[0] != '#' {
				t.Errorf("theme %s: %s = %q, want a #rrggbb hex", name, field, c)
			}
		}
	}
}

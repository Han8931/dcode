package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveVaultDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	base := "/base"

	cases := []struct {
		dir, want string
	}{
		{"", filepath.Join(base, "vault")},        // default
		{"~/notes", filepath.Join(home, "notes")}, // home-relative
		{"~", home}, // bare home
		{"my-notes", filepath.Join(base, "my-notes")}, // baseDir-relative
		{"/abs/notes/", "/abs/notes"},                 // absolute, cleaned
	}
	for _, c := range cases {
		got, err := resolveVaultDir(c.dir, base)
		if err != nil {
			t.Fatalf("resolveVaultDir(%q): %v", c.dir, err)
		}
		if got != c.want {
			t.Errorf("resolveVaultDir(%q) = %q; want %q", c.dir, got, c.want)
		}
	}
}

func TestLoadVaultDirFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[vault]\ndir = \"notes\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "notes"); cfg.VaultDir != want {
		t.Errorf("VaultDir = %q; want %q", cfg.VaultDir, want)
	}
}

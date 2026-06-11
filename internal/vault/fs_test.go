package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirsIncludesEmptyAndSkipsDot(t *testing.T) {
	root := t.TempDir()
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "Math", "Calc", "Derivatives.md"), "# D\n")
	if err := os.MkdirAll(filepath.Join(root, "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".obsidian", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}

	dirs, err := v.Dirs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Empty", "Math", "Math/Calc"}
	if len(dirs) != len(want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	for i := range want {
		if dirs[i] != want[i] {
			t.Fatalf("dirs = %v, want %v", dirs, want)
		}
	}
}

func TestDeleteRenameMakeDir(t *testing.T) {
	root := t.TempDir()
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "A", "one.md"), "# one\n")

	// Rename moves the note, creating the target's parents.
	if err := v.Rename("A/one.md", "B/C/one.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "B", "C", "one.md")); err != nil {
		t.Fatal("renamed note missing:", err)
	}
	// Rename refuses to clobber.
	mustWriteFile(t, filepath.Join(root, "A", "two.md"), "# two\n")
	if err := v.Rename("A/two.md", "B/C/one.md"); err == nil {
		t.Fatal("rename onto an existing file should fail")
	}

	if err := v.MakeDir("New/Deep"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "New", "Deep"))
	if err != nil || !fi.IsDir() {
		t.Fatal("MakeDir should create the directory")
	}

	// Delete removes a directory recursively.
	if err := v.Delete("B"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "B")); !os.IsNotExist(err) {
		t.Fatal("B should be gone")
	}
}

func TestFileOpsRejectEscapingPaths(t *testing.T) {
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", ".", "..", "../outside", "/etc/passwd", "a/../../x"} {
		if err := v.Delete(bad); err == nil {
			t.Errorf("Delete(%q) should be rejected", bad)
		}
		if err := v.MakeDir(bad); err == nil {
			t.Errorf("MakeDir(%q) should be rejected", bad)
		}
		if err := v.Rename(bad, "ok.md"); err == nil {
			t.Errorf("Rename(%q, ok.md) should be rejected", bad)
		}
		if err := v.Rename("ok.md", bad); err == nil {
			t.Errorf("Rename(ok.md, %q) should be rejected", bad)
		}
	}
}

package vault

// fs.go holds the vault's filesystem operations beyond note read/write: the
// directory listing that lets UIs mirror the on-disk structure, and the
// delete/rename/mkdir trio behind NERDTree-style file management. All paths
// are vault-relative and validated so an operation can never escape the root.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// safeAbs resolves relPath inside the vault root, rejecting paths that escape
// it ("../…", absolute paths) or that target the root itself.
func (v *Vault) safeAbs(relPath string) (string, error) {
	rel := filepath.Clean(filepath.FromSlash(relPath))
	if rel == "." || rel == "" || filepath.IsAbs(rel) ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid vault path %q", relPath)
	}
	return filepath.Join(v.root, rel), nil
}

// Dirs returns every directory under the root (vault-relative, sorted),
// including empty ones, so a tree view shows the user's real structure. The
// ignore rules (built-in defaults + .gitignore) are applied, so dependency and
// build directories are skipped.
func (v *Vault) Dirs() ([]string, error) {
	var dirs []string
	err := v.walk(func(rel string, isDir bool) error {
		if isDir {
			dirs = append(dirs, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	return dirs, nil
}

// Delete removes the note or directory (recursively) at relPath.
func (v *Vault) Delete(relPath string) error {
	abs, err := v.safeAbs(relPath)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

// Rename moves a note or directory to a new vault-relative path, creating
// parent directories as needed. It refuses to clobber an existing target.
func (v *Vault) Rename(oldRel, newRel string) error {
	oldAbs, err := v.safeAbs(oldRel)
	if err != nil {
		return err
	}
	newAbs, err := v.safeAbs(newRel)
	if err != nil {
		return err
	}
	if _, err := os.Stat(newAbs); err == nil {
		return fmt.Errorf("%s already exists", newRel)
	}
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		return err
	}
	return os.Rename(oldAbs, newAbs)
}

// MakeDir creates a directory (and any missing parents) at relPath.
func (v *Vault) MakeDir(relPath string) error {
	abs, err := v.safeAbs(relPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0o755)
}

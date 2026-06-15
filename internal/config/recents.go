package config

// recents.go persists the list of recently opened projects, globally per user
// (projects are not tied to a working directory), so the in-app project picker
// can offer them. It also resolves user-typed project paths.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxRecents bounds the stored recent-projects list.
const maxRecents = 12

// recentsFile is the global recents store, under the user's config dir
// (e.g. ~/.config/dcode/recents.json), falling back to ~/.dcode/recents.json.
func recentsFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", herr
		}
		dir = filepath.Join(home, ".dcode")
	} else {
		dir = filepath.Join(dir, "dcode")
	}
	return filepath.Join(dir, "recents.json"), nil
}

// LoadRecents reads the recent-projects list, most-recent first. A missing or
// unreadable file yields an empty list rather than an error.
func LoadRecents() []string {
	path, err := recentsFile()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return nil
	}
	return list
}

// AddRecent moves path to the front of the recent-projects list (deduped and
// capped) and persists it, returning the updated list. A write error is returned
// but the in-memory list is still valid.
func AddRecent(path string) ([]string, error) {
	out := mergeRecent(LoadRecents(), path)
	file, err := recentsFile()
	if err != nil {
		return out, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return out, err
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	return out, os.WriteFile(file, data, 0o644)
}

// mergeRecent returns list with path moved to the front, deduplicated, and capped
// to maxRecents. Pure (no I/O) so it is easy to test.
func mergeRecent(list []string, path string) []string {
	out := make([]string, 0, len(list)+1)
	out = append(out, path)
	for _, p := range list {
		if p != path {
			out = append(out, p)
		}
	}
	if len(out) > maxRecents {
		out = out[:maxRecents]
	}
	return out
}

// ResolveDir resolves a user-typed directory path to an absolute path: "~/…"
// expands to the home directory and a relative path is taken against the current
// working directory. Empty input is an error.
func ResolveDir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("no path given")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
	}
	return filepath.Abs(p)
}

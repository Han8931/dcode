// Package vault is d-code's rooted file store. It reads a directory of files:
// markdown notes (a .md file with an optional YAML frontmatter header followed by
// a markdown body) and read-only SOURCE files (any allowlisted code extension,
// whose whole content is the body — see IsSource). Notes reference each other
// with [[wikilinks]].
//
// This package is deliberately self-contained: it knows about files, frontmatter,
// and wikilinks, but nothing about the LLM or any UI. Both the TUI and the web
// front-end reach it through internal/core, which mounts two vault roots (a
// read-only source tree and a writable notes tree).
package vault

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Note is a single markdown note: its frontmatter metadata plus the markdown
// body. Path/RelPath locate it on disk and are not serialized into the file.
type Note struct {
	// Location (not part of the file contents).
	Path    string `yaml:"-"` // absolute path on disk; empty for an in-memory note
	RelPath string `yaml:"-"` // path relative to the vault root, e.g. "Math/Derivatives.md"

	// Frontmatter.
	ID      string         `yaml:"id,omitempty"`
	Title   string         `yaml:"title,omitempty"`
	Subject string         `yaml:"subject,omitempty"`
	Tags    []string       `yaml:"tags,omitempty"`
	Created string         `yaml:"created,omitempty"` // ISO date (YYYY-MM-DD)
	Source  string         `yaml:"source,omitempty"`  // "user" | "ai-generated" | "imported:<id>"
	Extra   map[string]any `yaml:"-"`                 // preserved unknown frontmatter keys (e.g. srs:)

	// Markdown body (everything after the frontmatter block).
	Body string `yaml:"-"`
}

// Known frontmatter keys, so ParseNote can route everything else into Extra and
// Marshal can round-trip it without loss.
var knownFrontmatterKeys = map[string]bool{
	"id": true, "title": true, "subject": true,
	"tags": true, "created": true, "source": true,
}

// sourceExts is the allowlist of non-markdown files d-code browses and opens —
// read-only, so the user's source is never rewritten with note frontmatter.
// The whole file becomes the note Body; ParseNote skips frontmatter for these.
var sourceExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".java": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".rs": true, ".rb": true, ".php": true, ".cs": true, ".kt": true, ".swift": true,
	".scala": true, ".sh": true, ".bash": true, ".zsh": true, ".sql": true,
	".html": true, ".css": true, ".scss": true, ".json": true, ".yaml": true,
	".yml": true, ".toml": true, ".xml": true, ".lua": true, ".r": true, ".dart": true,
}

// IsSource reports whether relPath names a browsable source file (read-only).
func IsSource(relPath string) bool {
	return sourceExts[strings.ToLower(filepath.Ext(relPath))]
}

// listable reports whether a filename should appear in the vault: markdown
// notes plus the allowlisted source files.
func listable(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || sourceExts[ext]
}

// Vault is a rooted directory of markdown notes.
type Vault struct {
	root string
}

// Open returns a Vault rooted at dir, creating the directory if needed.
func Open(dir string) (*Vault, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Vault{root: dir}, nil
}

// Root returns the vault's base directory.
func (v *Vault) Root() string { return v.root }

// --- parsing & serialization (pure, no I/O) ---

var frontmatterRE = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)

// ParseNote parses raw file bytes into a Note. relPath records where the note
// lives relative to the vault root. A file with no frontmatter is still a valid
// note: its whole content becomes the body. So is a file whose frontmatter
// block isn't valid YAML — vaults pointed at pre-existing notes (vault.dir in
// config) contain hand-written headers like "tags:git" (no space, a YAML
// scalar, not a map); those notes keep the block in their body, Obsidian-style,
// rather than failing the whole vault.
func ParseNote(relPath string, raw []byte) (Note, error) {
	// Source files are read verbatim: no frontmatter, the whole file is the body,
	// the title is the filename. Source:"code" marks them read-only to callers.
	if IsSource(relPath) {
		return Note{
			RelPath: relPath,
			Title:   filepath.Base(relPath), // keep the extension, e.g. "core.go"
			Source:  "code",
			Body:    string(raw),
		}, nil
	}

	n := Note{RelPath: relPath}
	text := string(raw)

	if m := frontmatterRE.FindStringSubmatch(text); m != nil {
		var fm map[string]any
		if err := yaml.Unmarshal([]byte(m[1]), &fm); err != nil {
			n.Body = strings.TrimLeft(text, "\n")
			n.Title = titleFromBodyOrPath(n.Body, relPath)
			return n, nil
		}
		n.ID = stringField(fm, "id")
		n.Title = stringField(fm, "title")
		n.Subject = stringField(fm, "subject")
		n.Created = stringField(fm, "created")
		n.Source = stringField(fm, "source")
		n.Tags = stringSlice(fm["tags"])
		for k, val := range fm {
			if !knownFrontmatterKeys[k] {
				if n.Extra == nil {
					n.Extra = map[string]any{}
				}
				n.Extra[k] = val
			}
		}
		text = text[len(m[0]):]
	}

	n.Body = strings.TrimLeft(text, "\n")
	if n.Title == "" {
		n.Title = titleFromBodyOrPath(n.Body, relPath)
	}
	return n, nil
}

// Marshal renders a Note back to file bytes: a YAML frontmatter block followed by
// the markdown body. Known keys are emitted in a stable order, then any Extra
// keys (sorted) so round-tripping is deterministic.
func (n Note) Marshal() []byte {
	fm := map[string]any{}
	put := func(k, val string) {
		if val != "" {
			fm[k] = val
		}
	}
	put("id", n.ID)
	put("title", n.Title)
	put("subject", n.Subject)
	if len(n.Tags) > 0 {
		fm["tags"] = n.Tags
	}
	put("created", n.Created)
	put("source", n.Source)
	for k, val := range n.Extra {
		if !knownFrontmatterKeys[k] {
			fm[k] = val
		}
	}

	var b bytes.Buffer
	if len(fm) > 0 {
		b.WriteString("---\n")
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		// yaml.Marshal of a map already sorts keys alphabetically, which is
		// deterministic; that's good enough for a stable on-disk form.
		_ = enc.Encode(fm)
		enc.Close()
		b.WriteString("---\n\n")
	}
	b.WriteString(strings.TrimLeft(n.Body, "\n"))
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// --- disk operations ---

// Read loads and parses the note at relPath.
func (v *Vault) Read(relPath string) (Note, error) {
	abs := filepath.Join(v.root, relPath)
	raw, err := os.ReadFile(abs)
	if err != nil {
		return Note{}, err
	}
	n, err := ParseNote(relPath, raw)
	if err != nil {
		return Note{}, err
	}
	n.Path = abs
	return n, nil
}

// Write saves a note to disk. If n.RelPath is empty it is derived from the
// subject (as a folder) and title (as the filename). Missing ID/Created are
// filled in. The (possibly updated) note is returned so callers can pick up the
// derived path and generated fields.
func (v *Vault) Write(n Note) (Note, error) {
	if n.ID == "" {
		n.ID = Slug(n.Title)
	}
	if n.Created == "" {
		n.Created = time.Now().Format("2006-01-02")
	}
	if n.RelPath == "" {
		n.RelPath = DeriveRelPath(n.Subject, n.Title)
	}
	abs := filepath.Join(v.root, n.RelPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Note{}, err
	}
	if err := os.WriteFile(abs, n.Marshal(), 0o644); err != nil {
		return Note{}, err
	}
	n.Path = abs
	return n, nil
}

// List walks the vault and returns every markdown note and source file, sorted
// by RelPath. Dot-directories (e.g. ".dcode") are skipped so app-managed files
// never appear.
func (v *Vault) List() ([]Note, error) {
	var notes []Note
	err := filepath.WalkDir(v.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != v.root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !listable(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(v.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n, err := ParseNote(rel, raw)
		if err != nil {
			return err
		}
		n.Path = path
		notes = append(notes, n)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].RelPath < notes[j].RelPath })
	return notes, nil
}

// --- wikilinks ---

// Link is one [[Target]] or [[Target|alias]] reference found in a note body.
type Link struct {
	Target string // the linked note's title/id, trimmed
	Alias  string // display text after "|", or "" if none
}

var wikilinkRE = regexp.MustCompile(`\[\[([^\]\[]+?)\]\]`)

// ParseLinks extracts the wikilinks from a markdown body, in order of
// appearance. Duplicate targets are kept (callers dedupe if they need to).
func ParseLinks(body string) []Link {
	matches := wikilinkRE.FindAllStringSubmatch(body, -1)
	links := make([]Link, 0, len(matches))
	for _, m := range matches {
		inner := strings.TrimSpace(m[1])
		if inner == "" {
			continue
		}
		target, alias := inner, ""
		if i := strings.IndexByte(inner, '|'); i >= 0 {
			target = strings.TrimSpace(inner[:i])
			alias = strings.TrimSpace(inner[i+1:])
		}
		if target == "" {
			continue
		}
		links = append(links, Link{Target: target, Alias: alias})
	}
	return links
}

// --- helpers ---

var (
	slugUnsafe    = regexp.MustCompile(`[^a-z0-9._-]+`)
	slugMultiDash = regexp.MustCompile(`-{2,}`)
)

// Slug turns arbitrary text into a filesystem- and id-safe kebab string: runs of
// unsafe characters (including spaces) collapse to a single dash.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugUnsafe.ReplaceAllString(s, "-")
	s = slugMultiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_.")
	if s == "" {
		s = "untitled"
	}
	return s
}

// DeriveRelPath builds "Subject/Title.md" for a note, preserving readable casing
// in the filename (Obsidian-style) while sanitizing path-unsafe characters. With
// no subject the note lives at the vault root.
func DeriveRelPath(subject, title string) string {
	name := sanitizeFilename(title)
	if name == "" {
		name = "Untitled"
	}
	if subject == "" {
		return name + ".md"
	}
	dir := sanitizeFilename(subject)
	return filepath.ToSlash(filepath.Join(dir, name+".md"))
}

// CleanFilename strips path-unsafe characters from one path component, for
// callers composing folder/file names from titles (the same sanitizing
// DeriveRelPath applies).
func CleanFilename(s string) string {
	if c := sanitizeFilename(s); c != "" {
		return c
	}
	return "Untitled"
}

var filenameUnsafe = regexp.MustCompile(`[/\\:*?"<>|]+`)

// sanitizeFilename strips characters that are illegal in path components while
// keeping spaces and case for a human-readable Obsidian-like filename.
func sanitizeFilename(s string) string {
	s = filenameUnsafe.ReplaceAllString(strings.TrimSpace(s), "")
	return strings.TrimSpace(s)
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprint(v)
	}
	return ""
}

// stringSlice coerces a YAML value into a []string, accepting both a list and a
// single scalar (so `tags: calculus` and `tags: [a, b]` both work).
func stringSlice(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := strings.TrimSpace(fmt.Sprint(e)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		if s := strings.TrimSpace(t); s != "" {
			return []string{s}
		}
	}
	return nil
}

// DeriveTitle returns a sensible note title from its body (first markdown H1) or,
// failing that, its filename — for callers creating a note without frontmatter.
func DeriveTitle(body, relPath string) string { return titleFromBodyOrPath(body, relPath) }

// titleFromBodyOrPath derives a display title when frontmatter has none: the
// first markdown H1, else the filename without extension.
func titleFromBodyOrPath(body, relPath string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
		if line != "" {
			break
		}
	}
	base := filepath.Base(relPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

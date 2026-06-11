// Package core is d-code's headless engine: the shared brain that both the
// terminal UI and the web GUI drive. It owns the file-and-AI orchestration
// (list/open/save files and notes, explain code, compute backlinks, search,
// chat) over two roots — a read-only source tree and a writable notes tree
// (see roots.go) — and returns plain data: no Bubble Tea, no net/http, no
// presentation. Front-ends are thin layers that call these methods.
//
// The rule that keeps two front-ends in parity: business logic lives here, never
// in a UI handler, so the TUI and web UI gain new capabilities at once.
package core

import (
	"context"
	"path"
	"sort"
	"strings"

	"dcode/internal/tutor"
	"dcode/internal/vault"
)

// Service is the engine. Construct it with New and share one instance across a
// process; its collaborators are safe for the single-trusted-user model.
//
// d-code spans two roots (see roots.go): the read-only SOURCE tree the user is
// decoding (code), and the writable NOTES tree where explanations and notes are
// saved, mounted in the tree under notesPrefix. notes may be nil (e.g. tests),
// in which case writes are rejected.
type Service struct {
	code  *vault.Vault // the source tree being decoded (read-only)
	notes *vault.Vault // saved explanations and user notes (writable); may be nil
	tutor *tutor.Tutor
}

// New builds a Service over a source root, a notes root, and a tutor. notes may
// be nil for read-only/testing setups.
func New(code, notes *vault.Vault, t *tutor.Tutor) *Service {
	return &Service{code: code, notes: notes, tutor: t}
}

// Offline reports whether the tutor is running on built-in content (no provider).
func (s *Service) Offline() bool { return s.tutor.Offline() }

// NoteMeta is the lightweight view of a note used in lists, trees, and links.
type NoteMeta struct {
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Subject string   `json:"subject"`
	Tags    []string `json:"tags"`
}

// Note is a full note: its metadata plus the markdown body and provenance.
type Note struct {
	NoteMeta
	Body   string `json:"body"`
	Source string `json:"source"`
}

// --- notes ---

// ListNotes returns every note's metadata — vault and course material — sorted
// by path.
func (s *Service) ListNotes() ([]NoteMeta, error) {
	notes, err := s.allNotes()
	if err != nil {
		return nil, err
	}
	out := make([]NoteMeta, 0, len(notes))
	for _, n := range notes {
		out = append(out, metaOf(n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// TreeEntry is one node of the vault's on-disk structure: a directory or a
// markdown note. Paths are vault-relative and "/"-separated; Name is the
// display name (the base name, without ".md" for notes), so file-tree UIs
// mirror the learner's real layout.
type TreeEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

// Tree returns the combined file structure — source files from the code root and
// notes from the notes root, in one shared namespace — sorted by path with
// directories de-duplicated across the two roots.
func (s *Service) Tree() ([]TreeEntry, error) {
	files, err := s.code.List()
	if err != nil {
		return nil, err
	}
	dirs, err := s.code.Dirs()
	if err != nil {
		return nil, err
	}
	seenDir := map[string]bool{}
	out := make([]TreeEntry, 0, len(dirs)+len(files))
	for _, d := range dirs {
		seenDir[d] = true
		out = append(out, TreeEntry{Path: d, Name: path.Base(d), Dir: true})
	}
	for _, n := range files {
		if !vault.IsSource(n.RelPath) {
			continue // .md files in the source tree are not d-code's to manage
		}
		out = append(out, TreeEntry{Path: n.RelPath, Name: treeName(n.RelPath)})
	}
	for _, e := range s.notesTree() {
		if e.Dir && seenDir[e.Path] {
			continue // a dir present in both roots appears once
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// treeName is a file's display name: source files keep their extension (core.go),
// markdown notes drop the ".md" (Derivatives).
func treeName(relPath string) string {
	base := path.Base(relPath)
	if vault.IsSource(relPath) {
		return base
	}
	return strings.TrimSuffix(base, path.Ext(base))
}

// Delete removes a note, or a directory and everything in it.
func (s *Service) Delete(path string) error { return s.deletePath(path) }

// Rename moves a note or directory to a new vault-relative path.
func (s *Service) Rename(oldPath, newPath string) error { return s.renamePath(oldPath, newPath) }

// MakeDir creates a directory, so structure can be laid out before notes exist.
func (s *Service) MakeDir(path string) error { return s.makeDirPath(path) }

// OpenNote loads a single note by its vault-relative path.
func (s *Service) OpenNote(path string) (Note, error) {
	n, err := s.readNote(path)
	if err != nil {
		return Note{}, err
	}
	return Note{NoteMeta: metaOf(n), Body: n.Body, Source: n.Source}, nil
}

// SaveNote writes body to the note at path, preserving existing frontmatter or
// creating a fresh note (deriving a sensible title) when none exists.
func (s *Service) SaveNote(path, body string) (NoteMeta, error) {
	n, err := s.readNote(path)
	if err != nil {
		n = vault.Note{
			RelPath: path,
			Title:   vault.DeriveTitle(body, path),
			Source:  "user",
		}
	}
	n.Body = body
	saved, err := s.writeNote(n)
	if err != nil {
		return NoteMeta{}, err
	}
	return metaOf(saved), nil
}

// GenerateLesson turns a learn-request into a new AI-authored note saved in the
// vault, and returns its metadata. This is the headline "I want to learn X →
// owned, linkable note" flow.
func (s *Service) GenerateLesson(ctx context.Context, request string) (NoteMeta, error) {
	nc, err := s.tutor.GenerateNote(ctx, request)
	if err != nil {
		return NoteMeta{}, err
	}
	saved, err := s.writeToNotes(vault.Note{
		Title:   nc.Title,
		Subject: nc.Subject,
		Tags:    nc.Tags,
		Source:  "ai-generated",
		Body:    nc.Body,
	})
	if err != nil {
		return NoteMeta{}, err
	}
	return metaOf(saved), nil
}

// Backlinks returns the notes whose body links (via [[wikilink]]) to the note at
// path. Currently an in-memory scan of the vault; a SQLite index will back this
// later without changing the signature.
func (s *Service) Backlinks(path string) ([]NoteMeta, error) {
	target, err := s.readNote(path)
	if err != nil {
		return nil, err
	}
	notes, err := s.allNotes()
	if err != nil {
		return nil, err
	}
	var out []NoteMeta
	for _, n := range notes {
		if n.RelPath == target.RelPath {
			continue
		}
		for _, l := range vault.ParseLinks(n.Body) {
			if linkMatches(l.Target, n2target(target)) {
				out = append(out, metaOf(n))
				break
			}
		}
	}
	return out, nil
}

// Search returns notes whose title, subject, or body contains query
// (case-insensitive), sorted by path. A simple substring scan for now.
func (s *Service) Search(query string) ([]NoteMeta, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	notes, err := s.allNotes()
	if err != nil {
		return nil, err
	}
	var out []NoteMeta
	for _, n := range notes {
		if q == "" ||
			strings.Contains(strings.ToLower(n.Title), q) ||
			strings.Contains(strings.ToLower(n.Subject), q) ||
			strings.Contains(strings.ToLower(n.Body), q) {
			out = append(out, metaOf(n))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// --- tutor ---

// maxChatTurns bounds how much conversation is SENT to the model (the visible
// transcript keeps everything). Long sessions otherwise grow tokens without
// bound and overflow small local models' context windows.
const maxChatTurns = 24

// maxContextChars bounds the study-context block sent with each chat call.
const maxContextChars = 6000

// TrimTurns bounds a conversation to the most recent turns (see maxChatTurns).
func TrimTurns(history []tutor.ChatTurn) []tutor.ChatTurn {
	if len(history) > maxChatTurns {
		return history[len(history)-maxChatTurns:]
	}
	return history
}

// ClampContext bounds a study-context string to a model-friendly size.
func ClampContext(s string) string {
	r := []rune(s)
	if len(r) <= maxContextChars {
		return s
	}
	return string(r[:maxContextChars]) + "\n…(truncated)"
}

// Chat continues a free-form tutoring conversation. studyContext is what the
// learner is currently looking at (note body, challenge, code) so replies stay
// grounded; "" sends conversation only.
func (s *Service) Chat(ctx context.Context, studyContext string, history []tutor.ChatTurn) (string, error) {
	return s.ChatStream(ctx, studyContext, history, nil)
}

// ChatStream is Chat with incremental delivery: onDelta receives each reply
// chunk as the model produces it; the assembled reply is returned at the end.
func (s *Service) ChatStream(ctx context.Context, studyContext string, history []tutor.ChatTurn, onDelta func(string)) (string, error) {
	return s.tutor.ChatStream(ctx, ClampContext(studyContext), TrimTurns(history), onDelta)
}

// DefaultPolishInstruction is what :polish uses when given no instruction of
// its own: a light copy-edit that leaves meaning and structure alone.
const DefaultPolishInstruction = "Improve grammar, clarity, and flow. Keep the meaning, structure, and formatting intact."

const polishSystemPrompt = `You are a meticulous copy-editor for a personal Markdown note.
Apply the user's instruction to the note and return ONLY the revised note as Markdown.
Rules:
- Output the note text and nothing else: no preamble, no explanation, no "Here is…", and no code fence wrapping the whole note.
- Preserve the note's meaning unless the instruction says otherwise.
- Keep YAML frontmatter (--- … ---), fenced code blocks (verbatim), [[wikilinks]], and the heading structure intact unless the instruction asks to change them.
- Stay in Markdown.`

// PolishNote asks the model to revise markdown per instruction, streaming the
// rewrite through onDelta and returning the full revised text. It is
// selection-agnostic — body may be a whole note or any excerpt — so the same
// call backs both whole-note polishing and (later) editing a visual selection.
// An empty instruction falls back to DefaultPolishInstruction. The result is
// stripped of any code fence the model wraps the whole output in.
func (s *Service) PolishNote(ctx context.Context, body, instruction string, onDelta func(string)) (string, error) {
	if strings.TrimSpace(instruction) == "" {
		instruction = DefaultPolishInstruction
	}
	hist := []tutor.ChatTurn{{Role: "user", Content: "Instruction: " + instruction + "\n\nNote:\n" + body}}
	full, err := s.tutor.StreamConversation(ctx, polishSystemPrompt, hist, onDelta)
	if err != nil {
		return "", err
	}
	return stripNoteFence(full), nil
}

// stripNoteFence unwraps a whole-output ```…``` fence, which models sometimes
// add around a markdown document despite being told not to. A note that merely
// contains fenced code (fences in the middle) is left untouched.
func stripNoteFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return s
	}
	lines := strings.Split(t, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return s
	}
	return strings.Join(lines[1:len(lines)-1], "\n")
}

// --- helpers ---

func metaOf(n vault.Note) NoteMeta {
	return NoteMeta{Path: n.RelPath, Title: n.Title, Subject: n.Subject, Tags: n.Tags}
}

// linkTarget is the minimal identity a wikilink can resolve against.
type linkTarget struct {
	title, id, relPath string
}

func n2target(n vault.Note) linkTarget {
	return linkTarget{title: n.Title, id: n.ID, relPath: n.RelPath}
}

// linkMatches reports whether a wikilink target refers to the given note, by
// title (case-insensitive), id, or filename stem.
func linkMatches(target string, n linkTarget) bool {
	t := strings.TrimSpace(strings.ToLower(target))
	if t == "" {
		return false
	}
	if strings.EqualFold(t, n.title) || strings.EqualFold(t, n.id) {
		return true
	}
	stem := n.relPath
	if i := strings.LastIndexByte(stem, '/'); i >= 0 {
		stem = stem[i+1:]
	}
	stem = strings.TrimSuffix(stem, ".md")
	return strings.EqualFold(t, stem)
}

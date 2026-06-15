package core

// roots.go routes virtual vault paths between d-code's two on-disk roots, by
// file kind:
//
//   - the SOURCE root (s.code): the code the user is decoding. Read-only —
//     d-code never rewrites the user's source. Owns source files (vault.IsSource).
//   - the NOTES root (s.notes): saved explanations and user notes. Writable.
//     Owns markdown notes (and directories created for them).
//
// A path's kind decides its root: a source-file path (by extension) resolves to
// the read-only code root; anything else (a .md note, or a directory) resolves
// to the writable notes root. The two roots share one virtual namespace, so an
// explanation saved at "internal/core.go.md" shows in the tree right next to the
// "internal/core.go" it explains — while living in a separate dir on disk, so the
// user's repo stays clean. notes may be nil (tests/read-only), making writes fail
// with a clear error.

import (
	"fmt"
	"path"
	"strings"

	"dcode/internal/vault"
)

// allNotes lists both roots together: source files from the code root and notes
// from the notes root.
func (s *Service) allNotes() ([]vault.Note, error) {
	src, err := s.code.List()
	if err != nil {
		return nil, err
	}
	out := make([]vault.Note, 0, len(src))
	for _, n := range src {
		if vault.IsSource(n.RelPath) {
			out = append(out, n)
		}
	}
	if s.notes != nil {
		nn, err := s.notes.List()
		if err != nil {
			return nil, err
		}
		for _, n := range nn {
			if !vault.IsSource(n.RelPath) {
				out = append(out, n)
			}
		}
	}
	return out, nil
}

// readNote loads a note by path from whichever root owns it. A markdown note in
// the writable notes root is read from there; anything else — a project's own
// .md, a source file, or any other project file — belongs to the read-only code
// root and opens as a "code" note (read-only, whole file as the body, binary
// files shown as a placeholder).
func (s *Service) readNote(p string) (vault.Note, error) {
	if isMarkdown(p) && s.notes != nil && s.notes.Has(p) {
		return s.notes.Read(p)
	}
	if s.code != nil && s.code.Has(p) {
		return s.code.ReadSource(p)
	}
	if s.notes == nil {
		return vault.Note{}, fmt.Errorf("nothing at %s", p)
	}
	return s.notes.Read(p)
}

// writeNote saves a note. Only markdown notes are writable (they live in the
// notes root); every other project file is read-only source, so its path is
// rejected — d-code never rewrites the user's source.
func (s *Service) writeNote(n vault.Note) (vault.Note, error) {
	if !isMarkdown(n.RelPath) {
		return vault.Note{}, fmt.Errorf("%q is read-only source and can't be edited", n.RelPath)
	}
	return s.writeToNotes(n)
}

// isMarkdown reports whether p names a markdown note (the only writable kind).
func isMarkdown(p string) bool { return strings.EqualFold(path.Ext(p), ".md") }

// writeToNotes writes a note (its RelPath relative to the notes root, or empty to
// derive one) into the writable notes root.
func (s *Service) writeToNotes(n vault.Note) (vault.Note, error) {
	if s.notes == nil {
		return vault.Note{}, fmt.Errorf("no notes directory configured")
	}
	return s.notes.Write(n)
}

// deletePath removes a note or directory; only paths that exist in the writable
// notes root are mutable, so read-only source files (and source directories) are
// rejected rather than silently no-op'd.
func (s *Service) deletePath(p string) error {
	if s.notes == nil {
		return fmt.Errorf("nothing at %s", p)
	}
	if !s.notes.Exists(p) {
		return fmt.Errorf("%q is read-only source and can't be deleted", p)
	}
	return s.notes.Delete(p)
}

// renamePath moves a note or directory within the writable notes root. The
// source it would move must live in the notes root; read-only source is rejected.
func (s *Service) renamePath(oldPath, newPath string) error {
	if s.notes == nil {
		return fmt.Errorf("nothing at %s", oldPath)
	}
	if !s.notes.Exists(oldPath) {
		return fmt.Errorf("%q is read-only source and can't be moved", oldPath)
	}
	return s.notes.Rename(oldPath, newPath)
}

// makeDirPath creates a directory in the writable notes root.
func (s *Service) makeDirPath(p string) error {
	if s.notes == nil {
		return fmt.Errorf("no notes directory configured")
	}
	return s.notes.MakeDir(p)
}

// notesTree returns the notes root's directory and note entries (markdown only;
// source files belong to the code root), in the shared virtual namespace.
func (s *Service) notesTree() []TreeEntry {
	if s.notes == nil {
		return nil
	}
	dirs, err := s.notes.Dirs()
	if err != nil {
		return nil
	}
	files, err := s.notes.List()
	if err != nil {
		return nil
	}
	var out []TreeEntry
	for _, d := range dirs {
		out = append(out, TreeEntry{Path: d, Name: path.Base(d), Dir: true})
	}
	for _, n := range files {
		if vault.IsSource(n.RelPath) {
			continue
		}
		out = append(out, TreeEntry{Path: n.RelPath, Name: treeName(n.RelPath)})
	}
	return out
}

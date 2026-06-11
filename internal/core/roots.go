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

// readNote loads a note by path from whichever root owns its kind.
func (s *Service) readNote(p string) (vault.Note, error) {
	if vault.IsSource(p) {
		return s.code.Read(p)
	}
	if s.notes == nil {
		return vault.Note{}, fmt.Errorf("no notes directory configured (%s)", p)
	}
	return s.notes.Read(p)
}

// writeNote saves a note. The source tree is read-only, so a source-file path is
// rejected; everything else is written to the notes root.
func (s *Service) writeNote(n vault.Note) (vault.Note, error) {
	if vault.IsSource(n.RelPath) {
		return vault.Note{}, fmt.Errorf("%q is read-only source and can't be edited", n.RelPath)
	}
	return s.writeToNotes(n)
}

// writeToNotes writes a note (its RelPath relative to the notes root, or empty to
// derive one) into the writable notes root.
func (s *Service) writeToNotes(n vault.Note) (vault.Note, error) {
	if s.notes == nil {
		return vault.Note{}, fmt.Errorf("no notes directory configured")
	}
	return s.notes.Write(n)
}

// deletePath removes a note or directory; only the notes root is mutable.
func (s *Service) deletePath(p string) error {
	if vault.IsSource(p) {
		return fmt.Errorf("%q is read-only source and can't be deleted", p)
	}
	if s.notes == nil {
		return fmt.Errorf("nothing at %s", p)
	}
	return s.notes.Delete(p)
}

// renamePath moves a note or directory within the writable notes root.
func (s *Service) renamePath(oldPath, newPath string) error {
	if vault.IsSource(oldPath) {
		return fmt.Errorf("%q is read-only source and can't be moved", oldPath)
	}
	if s.notes == nil {
		return fmt.Errorf("nothing at %s", oldPath)
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

package core

// project.go is d-code's project-opening: building a Service for a source tree,
// and switching to a different one at runtime. Saved notes are kept per-project
// (under a notes base) so opening another project never mixes one project's
// explanations into another's file tree.

import (
	"path/filepath"

	"dcode/internal/tutor"
	"dcode/internal/vault"
)

// OpenProject builds a Service for the project rooted at codeDir. Saved notes
// live in a per-project subfolder of notesBase (see ProjectNotesDir), so two
// projects never share a notes tree. createCode creates a missing code directory
// — used only for the unconfigured default scratch vault; an explicit project
// path passes false so a mistyped path is reported instead of materialized.
func OpenProject(codeDir, notesBase string, t *tutor.Tutor, createCode bool) (*Service, error) {
	var (
		code *vault.Vault
		err  error
	)
	if createCode {
		code, err = vault.Open(codeDir)
	} else {
		code, err = vault.OpenSource(codeDir)
	}
	if err != nil {
		return nil, err
	}
	notes, err := vault.Open(ProjectNotesDir(notesBase, codeDir))
	if err != nil {
		return nil, err
	}
	return New(code, notes, t), nil
}

// ProjectNotesDir is where a project's notes are stored: a per-project subfolder
// of notesBase keyed by the project's absolute path, so each project keeps its
// own notes tree.
func ProjectNotesDir(notesBase, codeDir string) string {
	abs, err := filepath.Abs(codeDir)
	if err != nil {
		abs = codeDir
	}
	return filepath.Join(notesBase, vault.Slug(abs))
}

// Reopen builds a Service for a different project, reusing this Service's tutor.
// The caller swaps to the returned Service; the old one needs no cleanup.
func (s *Service) Reopen(codeDir, notesBase string) (*Service, error) {
	return OpenProject(codeDir, notesBase, s.tutor, false)
}

// ProjectRoot is the absolute path of the source tree currently open.
func (s *Service) ProjectRoot() string { return s.code.Root() }

// ProjectName is a short display name for the open project: its root's base name.
func (s *Service) ProjectName() string { return filepath.Base(s.code.Root()) }

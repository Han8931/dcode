package core

// explain.go is d-code's headline capability: explaining code, grounded in the
// open file AND — when a single file isn't enough — related files across the
// project, then optionally saving that explanation as a companion note.

import (
	"context"
	"path"
	"strconv"
	"strings"

	"dcode/internal/vault"
)

// Context budgets keep the explain request within small models' windows while
// still carrying enough surrounding code to be useful.
const (
	maxExplainCodeChars = 16000 // the file being explained
	maxProjectCtxChars  = 7000  // the related-files digest, total
	projectSiblingHead  = 40    // lines of each related file included
	refDefWindow        = 24    // lines shown around each referenced definition
	refDefLead          = 4     // lines of lead-in above the definition (doc comment, related decls)
	maxProjectSiblings  = 6     // how many directory-neighbour files to inline
	maxRefDefs          = 6     // how many referenced-definition files to inline
	maxProjectFileList  = 200   // how many source paths to list in the project map
)

func itoa(n int) string { return strconv.Itoa(n) }

// ExplainRequest describes what to explain: a file, optionally narrowed to a
// selected excerpt, with a language hint for the prompt.
type ExplainRequest struct {
	Path      string // virtual path of the file (source or note)
	Selection string // optional excerpt to center the explanation on
	Lang      string // syntax language hint (e.g. "go"); "" is fine
}

// ExplainStream streams an explanation of req's code into onDelta and returns the
// full text. It gathers context automatically — the whole file plus a digest of
// related project files — so the explanation reflects how the code fits together,
// not just the lines in view.
func (s *Service) ExplainStream(ctx context.Context, req ExplainRequest, onDelta func(string)) (string, error) {
	code := req.Selection
	whole := ""
	if n, err := s.readNote(req.Path); err == nil {
		whole = n.Body
	}
	// The full file is the grounding context; a selection narrows the focus.
	if strings.TrimSpace(req.Selection) == "" {
		code = whole
	}
	code = clampChars(code, maxExplainCodeChars)
	projectCtx := s.projectContext(req.Path)
	return s.tutor.ExplainStream(ctx, req.Lang, req.Selection, code, projectCtx, onDelta)
}

// projectContext builds cross-file context so the model understands the code as
// part of a project, not a single file in isolation:
//
//  1. a map of the whole project's source files (paths only, capped), so the
//     model knows the overall structure and what else exists;
//  2. the DEFINITIONS the file depends on — for each top-level symbol it
//     references that's defined elsewhere, a focused snippet of that definition,
//     a poor-man's "go to definition" (see symbols.go); and
//  3. the contents (truncated) of any remaining directory neighbours — its
//     package/module siblings — to fill out the budget.
//
// It reads only the read-only source root.
func (s *Service) projectContext(p string) string {
	if !vault.IsSource(p) {
		return ""
	}
	files, err := s.code.List()
	if err != nil {
		return ""
	}

	// (1) Project file map — all source paths, capped.
	var paths []string
	bodyOf := make(map[string]string, len(files))
	var target vault.Note
	for _, f := range files {
		if !vault.IsSource(f.RelPath) {
			continue
		}
		paths = append(paths, f.RelPath)
		bodyOf[f.RelPath] = f.Body
		if f.RelPath == p {
			target = f
		}
	}
	var b strings.Builder
	if len(paths) > 1 {
		b.WriteString("Project source files:\n")
		shown := paths
		if len(shown) > maxProjectFileList {
			shown = shown[:maxProjectFileList]
		}
		for _, fp := range shown {
			b.WriteString("  " + fp + "\n")
		}
		if len(paths) > len(shown) {
			b.WriteString("  … and " + itoa(len(paths)-len(shown)) + " more\n")
		}
	}

	included := map[string]bool{p: true} // never inline the target's own body

	// (2) Referenced definitions — the cross-file symbols the file actually uses.
	idx := buildSymbolIndex(files)
	refs := 0
	for _, rf := range referencedFiles(target, idx) {
		if refs >= maxRefDefs || b.Len() >= maxProjectCtxChars {
			break
		}
		if included[rf.file] {
			continue
		}
		included[rf.file] = true
		b.WriteString("\n--- " + rf.file + " (defines " + strings.Join(rf.names, ", ") + ") ---\n")
		b.WriteString(windowLines(bodyOf[rf.file], rf.line-refDefLead, refDefWindow))
		b.WriteString("\n")
		refs++
	}

	// (3) Directory neighbours' contents (the package the file lives in), for any
	// budget left after the referenced definitions.
	dir := path.Dir(p)
	n := 0
	for _, f := range files {
		if n >= maxProjectSiblings || b.Len() >= maxProjectCtxChars {
			break
		}
		if !vault.IsSource(f.RelPath) || included[f.RelPath] || path.Dir(f.RelPath) != dir {
			continue
		}
		included[f.RelPath] = true
		b.WriteString("\n--- " + f.RelPath + " ---\n")
		b.WriteString(headLines(f.Body, projectSiblingHead))
		b.WriteString("\n")
		n++
	}
	return clampChars(strings.TrimSpace(b.String()), maxProjectCtxChars)
}

// SaveExplanation writes an explanation as a companion markdown note. The note
// mirrors the source path (srcPath + ".md"), so in the tree it sits beside the
// file it explains — while physically living in the separate notes root, keeping
// the user's source repo clean.
func (s *Service) SaveExplanation(srcPath, explanation string) (NoteMeta, error) {
	notePath := srcPath + ".md"
	body := "# Explanation — " + path.Base(srcPath) + "\n\n" +
		"_Decoded by d-code from `" + srcPath + "`._\n\n" +
		strings.TrimSpace(explanation) + "\n"
	return s.SaveNote(notePath, body)
}

// --- helpers ---

// headLines returns the first n lines of s, marking truncation.
func headLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		return strings.Join(lines[:n], "\n") + "\n… (truncated)"
	}
	return s
}

// clampChars bounds s to max runes, marking truncation.
func clampChars(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n… (truncated)"
}

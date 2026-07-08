package core

// gotodef.go is d-code's "go to definition": given a symbol name and the file the
// cursor is in, it finds where that symbol is defined. It reuses the same
// per-file structural parsing as the repo map (fileDefs — Go's parser,
// Tree-sitter, or the regex fallback), so a jump is as accurate as the map. Like
// Vim's gd it is local-first: a definition in the current file wins over one
// elsewhere, and the least-ambiguous external definition otherwise.

import (
	"strings"

	"dcode/internal/vault"
)

// DefLoc locates a symbol's definition: the source file (vault-relative) and the
// 1-based line it is defined on.
type DefLoc struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Name string `json:"name"`
}

// Definition resolves symbol to its definition site, preferring a definition in
// fromPath (the file the cursor is in) and otherwise the first external one
// (files are path-sorted, so the result is stable). ok is false when the symbol
// is defined nowhere in the source tree.
func (s *Service) Definition(fromPath, symbol string) (loc DefLoc, ok bool, err error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return DefLoc{}, false, nil
	}
	files, err := s.code.List()
	if err != nil {
		return DefLoc{}, false, err
	}

	var here, elsewhere *DefLoc
	for _, f := range files {
		if !vault.IsSource(f.RelPath) {
			continue
		}
		for _, d := range fileDefs(f) {
			if d.name != symbol {
				continue
			}
			cand := DefLoc{Path: f.RelPath, Line: d.line, Name: d.name}
			if f.RelPath == fromPath {
				if here == nil {
					here = &cand // first definition in the current file wins (local-first)
				}
			} else if elsewhere == nil {
				elsewhere = &cand
			}
		}
	}
	switch {
	case here != nil:
		return *here, true, nil
	case elsewhere != nil:
		return *elsewhere, true, nil
	}
	return DefLoc{}, false, nil
}

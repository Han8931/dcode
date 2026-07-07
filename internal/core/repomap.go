package core

// repomap.go builds d-code's structural "repository map": a compact, whole-
// project listing of each source file's top-level definitions and their
// signatures, ranked so the most-referenced (most structurally important) files
// and symbols come first. It is the shared structural context behind :overview,
// :explain, and :diff — the model sees the SHAPE of the whole repo (breadth)
// within a small token budget, instead of the full text of a handful of files
// (depth). This is the "repo map" idea structural-awareness tools (e.g. pi.dev,
// aider) use to let a small local model reason about a codebase it can't fit in
// its context window.
//
// Go files are parsed with the standard library's go/parser for exact
// signatures; every other language falls back to the heuristic regexes in
// symbols.go. No cgo and no external parser, so the tool stays `go install`-clean
// and language-agnostic by default — a real-parser backend (Tree-sitter) can
// later slot in behind fileDefs without touching its callers.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"dcode/internal/vault"
)

// maxDisplayMapChars bounds the map rendered for direct display by :map — larger
// than the digests fed to the model, since here the whole structure is the point.
const maxDisplayMapChars = 12000

// RepoMap renders the project's structural repository map for direct display:
// every source file's top-level signatures, ranked most-referenced first. It
// calls no model — it is pure static analysis — so :map is instant and works
// offline. It errors only when the source tree can't be read or has no code.
func (s *Service) RepoMap() (string, error) {
	files, err := s.code.List()
	if err != nil {
		return "", err
	}
	out := renderRepoMap(buildRepoMap(files), nil, maxDisplayMapChars)
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no source files to map in %s", s.ProjectName())
	}
	return out, nil
}

// symDef is one top-level definition captured for the repo map: the signature
// line shown to the model (e.g. "func New(cfg config.AIConfig) *Tutor"), the
// name it defines, the 1-based line it is defined on, and refs = how many other
// files reference the name (its structural importance, filled by buildRepoMap).
type symDef struct {
	name string
	sig  string
	line int
	refs int
}

// mapFile is one file's slice of the repo map: its path, its definitions
// (ranked most-referenced first), and rank = the file's total inbound references.
type mapFile struct {
	path string
	defs []symDef
	rank int
}

// fileDefs extracts the top-level definitions of one source file, most precise
// backend first: Go's own parser for .go, Tree-sitter for the languages with a
// grammar (see treesitter.go), and the regex heuristic (symbols.go) for anything
// else — or when a real parse fails mid-edit — so the map degrades gracefully
// rather than dropping the file.
func fileDefs(f vault.Note) []symDef {
	if strings.HasSuffix(f.RelPath, ".go") {
		if defs, err := goDefs(f.Body); err == nil {
			return defs
		}
	} else if defs, err := tsDefs(f.RelPath, f.Body); err == nil {
		return defs
	}
	return heuristicDefs(f.Body)
}

// goDefs parses Go source and returns its top-level funcs, methods, and type
// declarations with exact signatures sliced from the source. Consts, vars, and
// imports are skipped — funcs and types are the structural backbone, and keeping
// the map to them keeps it dense with the definitions cross-file code depends on.
func goDefs(src string) ([]symDef, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	b := []byte(src)
	// slice returns the source between two positions, whitespace-collapsed, so a
	// multi-line signature renders as one clean line.
	slice := func(from, to token.Pos) string {
		a, c := fset.Position(from).Offset, fset.Position(to).Offset
		if a < 0 || c > len(b) || a >= c {
			return ""
		}
		return collapseWS(string(b[a:c]))
	}

	var defs []symDef
	for _, d := range file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			// Signature is everything up to the body's "{" (or the whole decl for a
			// bodyless declaration, e.g. an assembly stub).
			to := decl.End()
			if decl.Body != nil {
				to = decl.Body.Lbrace
			}
			defs = append(defs, symDef{
				name: decl.Name.Name,
				sig:  strings.TrimRight(slice(decl.Pos(), to), " {"),
				line: fset.Position(decl.Pos()).Line,
			})
		case *ast.GenDecl:
			if decl.Tok != token.TYPE {
				continue
			}
			for _, spec := range decl.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				defs = append(defs, symDef{
					name: ts.Name.Name,
					sig:  strings.TrimSpace("type " + ts.Name.Name + " " + typeKind(ts.Type)),
					line: fset.Position(ts.Pos()).Line,
				})
			}
		}
	}
	return defs, nil
}

// typeKind names a type declaration's shape for its one-line signature (struct,
// interface, an alias's underlying name, …) without inlining the whole body.
func typeKind(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	case *ast.Ident:
		return t.Name
	case *ast.MapType:
		return "map"
	case *ast.ArrayType:
		return "slice/array"
	case *ast.FuncType:
		return "func"
	default:
		return ""
	}
}

// heuristicDefs is the language-agnostic fallback: it reuses symbols.go's
// definition regexes but keeps the whole matched line as the signature.
func heuristicDefs(body string) []symDef {
	var defs []symDef
	for i, line := range strings.Split(body, "\n") {
		for _, re := range defPatterns {
			if m := re.FindStringSubmatch(line); m != nil {
				defs = append(defs, symDef{
					name: m[1],
					sig:  collapseWS(strings.TrimRight(strings.TrimSpace(line), "{")),
					line: i + 1,
				})
				break
			}
		}
	}
	return defs
}

// collapseWS folds every run of whitespace (including newlines) into one space,
// so a wrapped multi-line signature renders as a single tidy line.
func collapseWS(s string) string { return strings.Join(strings.Fields(s), " ") }

// buildRepoMap builds the ranked structural map of every source file: it
// extracts each file's definitions, then ranks both symbols and files by inbound
// references (how many OTHER files use each defined name), so the map leads with
// the code the rest of the project depends on. Reference resolution is the same
// token-intersection heuristic explain.go already relies on (see symbols.go).
func buildRepoMap(files []vault.Note) []mapFile {
	type entry struct {
		path string
		defs []symDef
	}
	var srcs []entry
	tokens := map[string]map[string]bool{} // path -> identifiers it uses
	for _, f := range files {
		if !vault.IsSource(f.RelPath) {
			continue
		}
		srcs = append(srcs, entry{path: f.RelPath, defs: fileDefs(f)})
		set := map[string]bool{}
		for _, t := range identRE.FindAllString(f.Body, -1) {
			set[t] = true
		}
		tokens[f.RelPath] = set
	}

	// refCount = files, other than the definer, whose identifiers include name.
	refCount := func(name, self string) int {
		n := 0
		for p, set := range tokens {
			if p != self && set[name] {
				n++
			}
		}
		return n
	}

	out := make([]mapFile, 0, len(srcs))
	for _, e := range srcs {
		rank := 0
		for i := range e.defs {
			e.defs[i].refs = refCount(e.defs[i].name, e.path)
			rank += e.defs[i].refs
		}
		sort.SliceStable(e.defs, func(i, j int) bool {
			if e.defs[i].refs != e.defs[j].refs {
				return e.defs[i].refs > e.defs[j].refs
			}
			return e.defs[i].line < e.defs[j].line
		})
		out = append(out, mapFile{path: e.path, defs: e.defs, rank: rank})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank > out[j].rank
		}
		return out[i].path < out[j].path
	})
	return out
}

// renderRepoMap renders the map as "path" lines each followed by indented
// signatures, stopping once budget runes are spent. When only is non-nil, just
// those paths are rendered (a focused map, e.g. the files a diff touches plus
// their callers). Files with no captured definitions are skipped.
func renderRepoMap(m []mapFile, only map[string]bool, budget int) string {
	var b strings.Builder
	for _, f := range m {
		if only != nil && !only[f.path] {
			continue
		}
		if len(f.defs) == 0 {
			continue
		}
		if b.Len() >= budget {
			b.WriteString("… (map truncated)\n")
			break
		}
		b.WriteString(f.path + "\n")
		for _, d := range f.defs {
			b.WriteString("  " + d.sig + "\n")
			if b.Len() >= budget {
				break
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// repoMapText renders the whole-project structural map within budget runes, or
// "" if the source tree can't be listed. Shared by :overview and :explain.
func (s *Service) repoMapText(budget int) string {
	files, err := s.code.List()
	if err != nil {
		return ""
	}
	return clampChars(renderRepoMap(buildRepoMap(files), nil, budget), budget)
}

// focusFiles returns the changed paths plus every source file that references a
// symbol the changed files define — the structural neighbourhood of a change,
// used to focus the :diff map on what the change actually touches and affects.
func focusFiles(m []mapFile, files []vault.Note, changed map[string]bool) map[string]bool {
	changedNames := map[string]bool{}
	for _, f := range m {
		if changed[f.path] {
			for _, d := range f.defs {
				changedNames[d.name] = true
			}
		}
	}
	focus := map[string]bool{}
	for p := range changed {
		focus[p] = true
	}
	for _, f := range files {
		if !vault.IsSource(f.RelPath) || focus[f.RelPath] {
			continue
		}
		for _, t := range identRE.FindAllString(f.Body, -1) {
			if changedNames[t] {
				focus[f.RelPath] = true
				break
			}
		}
	}
	return focus
}

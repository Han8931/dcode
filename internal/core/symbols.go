package core

// symbols.go gives explain.go a poor-man's "go to definition": a lightweight,
// language-agnostic index of where each source file defines its top-level
// symbols, used to pull the DEFINITIONS a file depends on into the explain
// context — not just its directory neighbours. It is heuristic (regex over
// definition keywords, no real parser), so it favours recall over precision: a
// missed or spurious symbol only changes which extra context the model sees.

import (
	"regexp"
	"sort"
	"strings"

	"dcode/internal/vault"
)

// defPatterns match common top-level definition forms across languages,
// capturing the defined name. They run per line, so "^" anchors a line start.
// The first covers the func/fn/function/def/class/type/interface/struct/enum/
// trait family (with optional export/visibility/async modifiers); the second
// covers Go methods, whose name follows the receiver: "func (r *T) Name(".
var defPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:public\s+|private\s+|protected\s+|static\s+|final\s+|abstract\s+)*(?:async\s+)?(?:func|fn|function|def|class|type|interface|struct|enum|trait)\s+(\w+)`),
	regexp.MustCompile(`^\s*func\s+\([^)]*\)\s+(\w+)`),
}

// identRE tokenizes a file into bare identifiers, so references can be resolved
// by intersecting a file's tokens with the symbol index.
var identRE = regexp.MustCompile(`[A-Za-z_]\w{1,}`)

// symbolLoc is one definition site: the file that defines a symbol and the line
// (1-based) it is defined on.
type symbolLoc struct {
	file string
	line int
}

// buildSymbolIndex maps every top-level symbol name to the source files (and
// lines) that define it. Markdown and non-source files are skipped.
func buildSymbolIndex(files []vault.Note) map[string][]symbolLoc {
	idx := map[string][]symbolLoc{}
	for _, f := range files {
		if !vault.IsSource(f.RelPath) {
			continue
		}
		for i, line := range strings.Split(f.Body, "\n") {
			for _, re := range defPatterns {
				if m := re.FindStringSubmatch(line); m != nil {
					idx[m[1]] = append(idx[m[1]], symbolLoc{file: f.RelPath, line: i + 1})
				}
			}
		}
	}
	return idx
}

// refFile is a project file the target references: the file, the line of the
// most-specific referenced symbol it defines, the symbol names referenced, and
// score = the lowest ambiguity (fewest defining files) among those names, so the
// most uniquely-resolved dependencies rank first.
type refFile struct {
	file  string
	line  int
	names []string
	score int
}

// referencedFiles resolves the cross-file dependencies of target: identifiers in
// its body that name a top-level symbol defined in ANOTHER source file. Results
// are grouped by defining file and ordered most-specific first (symbols with a
// single definition site beat widely-defined names like "New"), so the limited
// context budget is spent on the clearest dependencies.
func referencedFiles(target vault.Note, idx map[string][]symbolLoc) []refFile {
	if target.Body == "" {
		return nil
	}
	tokens := map[string]bool{}
	for _, t := range identRE.FindAllString(target.Body, -1) {
		tokens[t] = true
	}

	byFile := map[string]*refFile{}
	nameSeen := map[string]map[string]bool{} // file -> set of names already recorded
	for name := range tokens {
		locs := idx[name]
		if len(locs) == 0 {
			continue
		}
		amb := len(locs) // how many files define this name (ambiguity)
		for _, loc := range locs {
			if loc.file == target.RelPath {
				continue // a file's own definitions aren't cross-file context
			}
			rf := byFile[loc.file]
			if rf == nil {
				rf = &refFile{file: loc.file, line: loc.line, score: amb}
				byFile[loc.file] = rf
				nameSeen[loc.file] = map[string]bool{}
			}
			if !nameSeen[loc.file][name] {
				rf.names = append(rf.names, name)
				nameSeen[loc.file][name] = true
			}
			// Anchor the snippet on the least-ambiguous (most specific) symbol.
			if amb < rf.score {
				rf.score, rf.line = amb, loc.line
			}
		}
	}

	out := make([]refFile, 0, len(byFile))
	for _, rf := range byFile {
		sort.Strings(rf.names)
		out = append(out, *rf)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score < out[j].score
		}
		return out[i].file < out[j].file
	})
	return out
}

// windowLines returns n lines of body starting at the 1-based start line,
// marking truncation when more follows — a focused view of one definition.
func windowLines(body string, start, n int) string {
	lines := strings.Split(body, "\n")
	i := start - 1
	if i < 0 {
		i = 0
	}
	end := i + n
	if end > len(lines) {
		end = len(lines)
	}
	out := strings.Join(lines[i:end], "\n")
	if end < len(lines) {
		out += "\n… (truncated)"
	}
	return out
}

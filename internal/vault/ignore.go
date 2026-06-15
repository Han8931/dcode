package vault

// ignore.go decides which directories and files the vault walk skips, so that
// pointing d-code at a real project doesn't drag in (or read) build output and
// dependency trees. Two layers stack:
//
//   - built-in defaults: a curated set of heavy directories (.git, node_modules,
//     target, …) and junk files (.DS_Store) that are never worth showing; and
//   - the project's own .gitignore files, honored per-directory like git does —
//     a deeper .gitignore overrides a shallower one, a later line overrides an
//     earlier one, and a leading "!" re-includes.
//
// The matcher is intentionally a practical subset of the gitignore spec (the
// common forms: *, **, ?, [..], leading "/" anchoring, trailing "/" dir-only,
// "!" negation). It is good enough to keep the tree clean without pulling in a
// dependency; exotic patterns may match more loosely than git.

import (
	"regexp"
	"strings"
)

// defaultIgnoreDirs are directory names skipped everywhere, regardless of any
// .gitignore. These are build output, dependency caches, and editor/VCS metadata
// that no one browses as source. Kept conservative: names that are commonly real
// source dirs (bin, out, env) are intentionally absent and left to .gitignore.
var defaultIgnoreDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "bower_components": true,
	"vendor": true, "target": true, "dist": true,
	".next": true, ".nuxt": true, ".svelte-kit": true, ".turbo": true, ".parcel-cache": true,
	".venv": true, "venv": true, "__pycache__": true,
	".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true, ".tox": true, ".eggs": true,
	".gradle": true, ".mvn": true,
	".idea": true, ".vscode": true,
	".cache": true, "coverage": true, ".terraform": true,
	"Pods": true, "DerivedData": true, ".dart_tool": true, "elm-stuff": true,
	".obsidian": true, ".dcode": true, ".dcode-notes": true,
}

// defaultIgnoreFiles are individual filenames skipped everywhere: OS/editor junk
// that is never project content.
var defaultIgnoreFiles = map[string]bool{
	".DS_Store": true, "Thumbs.db": true, ".localized": true,
}

// gitignorePattern is one compiled .gitignore line.
type gitignorePattern struct {
	negate   bool           // leading "!" — re-includes a previously ignored path
	dirOnly  bool           // trailing "/" — matches directories only
	matchAbs bool           // pattern is anchored to the .gitignore's dir (vs. basename anywhere)
	re       *regexp.Regexp // matches the candidate (full rel path if matchAbs, else basename)
}

// gitignore is the compiled patterns from a single .gitignore file, applied
// relative to the directory that file lives in.
type gitignore struct {
	patterns []gitignorePattern
}

// parseGitignore compiles the lines of one .gitignore file.
func parseGitignore(data []byte) *gitignore {
	g := &gitignore{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(strings.TrimSuffix(raw, "\r"), " ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := gitignorePattern{}
		if strings.HasPrefix(line, "!") {
			p.negate = true
			line = line[1:]
		}
		// A leading "\" escapes a literal "!" or "#"; we only need the "!" case.
		if strings.HasPrefix(line, `\`) {
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}
		// A "/" anywhere except a sole trailing one anchors the pattern to the
		// .gitignore's directory; otherwise it matches a basename at any depth.
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		if strings.Contains(line, "/") {
			anchored = true
		}
		p.matchAbs = anchored
		p.re = compileGlob(line)
		g.patterns = append(g.patterns, p)
	}
	return g
}

// match reports whether rel (a slash path relative to this .gitignore's
// directory) is ignored, and whether any pattern matched it at all. Later
// patterns win; a negating pattern re-includes.
func (g *gitignore) match(rel string, isDir bool) (ignored, matched bool) {
	base := rel
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		base = rel[i+1:]
	}
	for _, p := range g.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		target := base
		if p.matchAbs {
			target = rel
		}
		if p.re.MatchString(target) {
			ignored = !p.negate
			matched = true
		}
	}
	return ignored, matched
}

// compileGlob turns one gitignore glob into an anchored regexp. It honors the
// segment-aware wildcards: "*" and "?" do not cross "/", "**" does, and "[..]"
// is a character class.
func compileGlob(glob string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	r := []rune(glob)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch c {
		case '*':
			if i+1 < len(r) && r[i+1] == '*' {
				i++ // consume the second '*'
				if i+1 < len(r) && r[i+1] == '/' {
					i++                       // consume the '/'
					b.WriteString("(?:.*/)?") // "**/" → any number of leading dirs
				} else {
					b.WriteString(".*") // "**" → anything, crossing "/"
				}
			} else {
				b.WriteString("[^/]*") // "*" → anything within a segment
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			b.WriteRune('[')
			i++
			if i < len(r) && (r[i] == '!' || r[i] == '^') {
				b.WriteRune('^')
				i++
			}
			for i < len(r) && r[i] != ']' {
				if r[i] == '\\' {
					b.WriteString(`\\`)
				} else {
					b.WriteRune(r[i])
				}
				i++
			}
			b.WriteRune(']')
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '\\':
			b.WriteByte('\\')
			b.WriteRune(c)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// ignoreStack is the set of .gitignore files active at the current point of a
// walk: the root's, then each ancestor directory's. It is consulted with paths
// relative to the vault root and translates them to each frame's base.
type ignoreStack struct {
	frames []ignoreFrame
}

type ignoreFrame struct {
	dir string // slash path of this .gitignore's directory, relative to root ("" = root)
	gi  *gitignore
}

// push adds a directory's .gitignore (dir is root-relative, "" for the root).
func (s *ignoreStack) push(dir string, gi *gitignore) {
	if gi != nil && len(gi.patterns) > 0 {
		s.frames = append(s.frames, ignoreFrame{dir: dir, gi: gi})
	}
}

// truncateTo drops frames for directories no longer on the walk path (depth is
// the slash count of the current directory), so siblings don't inherit each
// other's .gitignore.
func (s *ignoreStack) truncateTo(dir string) {
	kept := s.frames[:0]
	for _, f := range s.frames {
		if f.dir == "" || dir == f.dir || strings.HasPrefix(dir+"/", f.dir+"/") {
			kept = append(kept, f)
		}
	}
	s.frames = kept
}

// ignored reports whether the root-relative path rel should be skipped, combining
// the built-in defaults with every active .gitignore (shallow first, deep wins).
func (s *ignoreStack) ignored(rel string, isDir bool) bool {
	base := rel
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		base = rel[i+1:]
	}
	if isDir && defaultIgnoreDirs[base] {
		return true
	}
	if !isDir && defaultIgnoreFiles[base] {
		return true
	}
	ignored := false
	for _, f := range s.frames {
		if f.dir != "" && rel != f.dir && !strings.HasPrefix(rel, f.dir+"/") {
			continue // this .gitignore's directory isn't an ancestor of rel
		}
		sub := rel
		if f.dir != "" {
			sub = strings.TrimPrefix(rel, f.dir+"/")
		}
		if g, ok := f.gi.match(sub, isDir); ok {
			ignored = g
		}
	}
	return ignored
}

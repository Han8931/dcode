package core

// treesitter.go extends the repo map's structural parsing beyond Go to other
// languages using Tree-sitter (github.com/smacker/go-tree-sitter). Go files keep
// using the standard library's go/parser (see repomap.go — no cgo needed for the
// tool's own language); this file handles Python, JavaScript, TypeScript/TSX, and
// Rust with real ASTs, so their signatures in the map are exact rather than the
// regex heuristic's best guess. Any language without a grammar here still falls
// back to that heuristic, so coverage only ever improves.
//
// Tree-sitter is a C library, so this file makes the build depend on cgo (a C
// compiler). That is the deliberate trade for real multi-language parsing.
//
// Extraction is driven by a small per-language table (tsSpec): which node types
// are definitions, which are containers to descend into for nested defs (class
// methods, impl blocks), and which are transparent wrappers to see through
// (Python decorators, JS/TS `export`). The name and signature come from the
// grammar's "name" and "body" fields, which are named consistently enough across
// these grammars for one generic walk to serve them all.

import (
	"context"
	"fmt"
	"path"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// tsSpec tells the walker how to read one language's tree: def = node types that
// are definitions (they carry a "name"); nest = containers whose body holds more
// defs to collect (a class's methods, a Rust impl/trait); unwrap = transparent
// wrappers to descend through without counting (a decorator, an `export`).
type tsSpec struct {
	lang   *sitter.Language
	def    map[string]bool
	nest   map[string]bool
	unwrap map[string]bool
}

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// tsSpecs maps a file extension to its language spec. Extensions absent here get
// the regex heuristic in symbols.go instead.
var tsSpecs = map[string]tsSpec{
	".py": {
		lang:   python.GetLanguage(),
		def:    set("function_definition", "class_definition"),
		nest:   set("class_definition"),
		unwrap: set("decorated_definition"),
	},
	".js":  jsSpec(javascript.GetLanguage()),
	".jsx": jsSpec(javascript.GetLanguage()),
	".ts":  tsSpecFor(typescript.GetLanguage()),
	".tsx": tsSpecFor(tsx.GetLanguage()),
	".rs": {
		lang: rust.GetLanguage(),
		def: set("function_item", "function_signature_item", "struct_item",
			"enum_item", "trait_item", "mod_item", "type_item"),
		nest:   set("trait_item", "impl_item", "mod_item"),
		unwrap: set(),
	},
}

func jsSpec(lang *sitter.Language) tsSpec {
	return tsSpec{
		lang: lang,
		def: set("function_declaration", "generator_function_declaration",
			"class_declaration", "method_definition"),
		nest:   set("class_declaration"),
		unwrap: set("export_statement"),
	}
}

func tsSpecFor(lang *sitter.Language) tsSpec {
	return tsSpec{
		lang: lang,
		def: set("function_declaration", "class_declaration", "abstract_class_declaration",
			"method_definition", "interface_declaration", "type_alias_declaration",
			"enum_declaration"),
		nest:   set("class_declaration", "abstract_class_declaration"),
		unwrap: set("export_statement"),
	}
}

// tsDefs parses one source file with Tree-sitter and returns its top-level (and
// one-level-nested) definitions with exact signatures. It errors when the file's
// extension has no grammar, so fileDefs can fall back to the heuristic.
func tsDefs(relPath, src string) ([]symDef, error) {
	spec, ok := tsSpecs[strings.ToLower(path.Ext(relPath))]
	if !ok {
		return nil, fmt.Errorf("no tree-sitter grammar for %s", relPath)
	}
	p := sitter.NewParser()
	p.SetLanguage(spec.lang)
	tree, err := p.ParseCtx(context.Background(), nil, []byte(src))
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	var out []symDef
	tsCollect(tree.RootNode(), []byte(src), spec, &out)
	return out, nil
}

// tsCollect walks a node's named children, collecting definitions per spec: it
// sees through wrappers, records defs, and descends into containers (and defs
// that are also containers, e.g. a class) one level to pick up their members.
// Function bodies are never containers, so local/nested defs stay out of the map.
func tsCollect(n *sitter.Node, src []byte, spec tsSpec, out *[]symDef) {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		t := c.Type()
		switch {
		case spec.unwrap[t]:
			tsCollect(c, src, spec, out) // see through: stay at the same level
		case spec.def[t]:
			*out = append(*out, tsDef(c, src))
			if spec.nest[t] {
				descend(c, src, spec, out)
			}
		case spec.nest[t]:
			descend(c, src, spec, out) // a container with no name of its own (Rust impl)
		}
	}
}

// descend collects definitions from a container node's "body" child.
func descend(c *sitter.Node, src []byte, spec tsSpec, out *[]symDef) {
	if body := c.ChildByFieldName("body"); body != nil {
		tsCollect(body, src, spec, out)
	}
}

// tsDef turns a definition node into a symDef: its "name" field, and a signature
// sliced from the node's start up to its body (so "func f(x) {…}" becomes
// "func f(x)"), collapsed to one line.
func tsDef(c *sitter.Node, src []byte) symDef {
	name := "?"
	if nn := c.ChildByFieldName("name"); nn != nil {
		name = nn.Content(src)
	}
	end := c.EndByte()
	if body := c.ChildByFieldName("body"); body != nil {
		end = body.StartByte()
	}
	sig := collapseWS(strings.TrimRight(strings.TrimSpace(string(src[c.StartByte():end])), " {"))
	return symDef{name: name, sig: sig, line: int(c.StartPoint().Row) + 1}
}

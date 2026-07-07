package core

import (
	"testing"

	"dcode/internal/vault"
)

// sigOf indexes a def slice by name for readable assertions.
func sigOf(defs []symDef) map[string]string {
	m := map[string]string{}
	for _, d := range defs {
		m[d.name] = d.sig
	}
	return m
}

func TestTreeSitterPython(t *testing.T) {
	defs, err := tsDefs("app.py",
		"def greet(name):\n    return name\n\n"+
			"@decorator\n"+
			"class Dog:\n    def bark(self, loud=False):\n        pass\n")
	if err != nil {
		t.Fatal(err)
	}
	got := sigOf(defs)
	if got["greet"] != "def greet(name):" {
		t.Errorf("greet sig = %q", got["greet"])
	}
	if got["Dog"] != "class Dog:" { // decorated class is seen through the decorator
		t.Errorf("Dog sig = %q", got["Dog"])
	}
	if got["bark"] != "def bark(self, loud=False):" { // method captured one level in
		t.Errorf("bark sig = %q", got["bark"])
	}
}

func TestTreeSitterTypeScript(t *testing.T) {
	defs, err := tsDefs("mod.ts",
		"export function add(a: number, b: number): number { return a + b }\n"+
			"interface Shape { area(): number }\n"+
			"type ID = string\n"+
			"class Box { open(): void {} }\n")
	if err != nil {
		t.Fatal(err)
	}
	got := sigOf(defs)
	if got["add"] != "function add(a: number, b: number): number" { // export unwrapped, return type kept
		t.Errorf("add sig = %q", got["add"])
	}
	if got["Shape"] != "interface Shape" {
		t.Errorf("Shape sig = %q", got["Shape"])
	}
	if got["ID"] != "type ID = string" {
		t.Errorf("ID sig = %q", got["ID"])
	}
	if got["open"] != "open(): void" { // method one level into the class
		t.Errorf("open sig = %q", got["open"])
	}
}

func TestTreeSitterRust(t *testing.T) {
	defs, err := tsDefs("lib.rs",
		"pub fn add(a: i32, b: i32) -> i32 { a + b }\n"+
			"struct Point { x: i32, y: i32 }\n"+
			"impl Point { fn origin() -> Point { Point { x: 0, y: 0 } } }\n")
	if err != nil {
		t.Fatal(err)
	}
	got := sigOf(defs)
	if got["add"] != "pub fn add(a: i32, b: i32) -> i32" {
		t.Errorf("add sig = %q", got["add"])
	}
	if got["Point"] != "struct Point" {
		t.Errorf("Point sig = %q", got["Point"])
	}
	if got["origin"] != "fn origin() -> Point" { // method from inside the impl block
		t.Errorf("origin sig = %q", got["origin"])
	}
}

// fileDefs routes non-Go source through Tree-sitter, so a Python file's map
// entries are real signatures — not the regex heuristic's guess.
func TestFileDefsUsesTreeSitterForPython(t *testing.T) {
	defs := fileDefs(vault.Note{RelPath: "svc.py", Body: "def handler(req):\n    return 200\n"})
	if got := sigOf(defs)["handler"]; got != "def handler(req):" {
		t.Fatalf("fileDefs should parse python via tree-sitter, got %q", got)
	}
}

// A language with no grammar still yields definitions via the heuristic fallback.
func TestFileDefsFallsBackForUnsupported(t *testing.T) {
	defs := fileDefs(vault.Note{RelPath: "Main.java", Body: "class Main {\n  void run() {}\n}\n"})
	if len(defs) == 0 {
		t.Fatal("unsupported language should still yield heuristic defs")
	}
}

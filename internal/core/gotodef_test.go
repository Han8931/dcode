package core

import "testing"

func TestDefinitionResolves(t *testing.T) {
	s, _, _ := explainService(t, map[string]string{
		"app/main.go": "package app\n\nfunc run() {\n\tw := NewWidget()\n}\n\nfunc helper() {}\n",
		"ui/widget.go": "package ui\n\ntype Widget struct{}\n\n" +
			"func NewWidget() *Widget { return &Widget{} }\n",
	})

	// Cross-file: NewWidget is defined in ui/widget.go, at the func line (5).
	loc, ok, err := s.Definition("app/main.go", "NewWidget")
	if err != nil || !ok {
		t.Fatalf("expected to resolve NewWidget: ok=%v err=%v", ok, err)
	}
	if loc.Path != "ui/widget.go" || loc.Line != 5 {
		t.Fatalf("NewWidget → %s:%d, want ui/widget.go:5", loc.Path, loc.Line)
	}

	// Local-first: helper is defined in the current file, so resolve there.
	loc, ok, _ = s.Definition("app/main.go", "helper")
	if !ok || loc.Path != "app/main.go" {
		t.Fatalf("helper should resolve locally, got %+v ok=%v", loc, ok)
	}

	// Unknown symbol resolves to nothing (not an error).
	if _, ok, err := s.Definition("app/main.go", "Nope"); ok || err != nil {
		t.Fatalf("unknown symbol: ok=%v err=%v, want false/nil", ok, err)
	}

	// Empty symbol is a no-op, never an error.
	if _, ok, err := s.Definition("app/main.go", "  "); ok || err != nil {
		t.Fatalf("empty symbol: ok=%v err=%v, want false/nil", ok, err)
	}
}

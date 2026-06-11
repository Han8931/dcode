package editor

import "testing"

func TestCmdCompleterCycles(t *testing.T) {
	names := []string{"clear", "compact", "copy", "fold"}
	var c CmdCompleter

	// Tab cycles forward through the "c" matches, then back to the prefix.
	want := []string{"clear", "compact", "copy", "c", "clear"}
	for i, w := range want {
		got, ok := c.Next("c", names, 1)
		if !ok || got != w {
			t.Fatalf("Tab %d = %q, %v; want %q", i+1, got, ok, w)
		}
	}

	// Shift-Tab steps back from wherever the cycle is.
	if got, _ := c.Next("c", names, -1); got != "c" {
		t.Fatalf("Shift-Tab = %q; want %q", got, "c")
	}

	// After a Reset (the caller saw an edit), matching restarts from the new input.
	c.Reset()
	if got, ok := c.Next("fo", names, 1); !ok || got != "fold" {
		t.Fatalf("Next(fo) = %q, %v; want fold, true", got, ok)
	}

	// No match leaves the prompt untouched.
	c.Reset()
	if _, ok := c.Next("zz", names, 1); ok {
		t.Fatal("Next(zz) reported a match; want none")
	}
}

func TestCmdCompleterHint(t *testing.T) {
	names := []string{"clear", "compact", "copy"}
	var c CmdCompleter

	if h := c.Hint(); h != "" {
		t.Fatalf("idle Hint = %q; want empty", h)
	}
	c.Next("c", names, 1)
	if h := c.Hint(); h != "[clear]  compact  copy" {
		t.Fatalf("Hint = %q", h)
	}

	// A single match shows no menu — the completed text is feedback enough.
	c.Reset()
	c.Next("co", []string{"copy", "clear"}, 1)
	if h := c.Hint(); h != "" {
		t.Fatalf("single-match Hint = %q; want empty", h)
	}
}

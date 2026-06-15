package vault

import "testing"

func TestGitignoreMatching(t *testing.T) {
	gi := parseGitignore([]byte(`
# a comment
*.log
/dist
build/
node_modules
docs/*.tmp
!keep.log
`))
	cases := []struct {
		rel     string
		isDir   bool
		ignored bool
	}{
		{"app.log", false, true},          // *.log basename anywhere
		{"sub/deep/app.log", false, true}, // *.log at depth (basename)
		{"keep.log", false, false},        // re-included by !keep.log
		{"dist", true, true},              // /dist anchored to root
		{"sub/dist", true, false},         // /dist is anchored, not at depth
		{"build", true, true},             // build/ dir-only
		{"build", false, false},           // build/ does not match a file
		{"node_modules", true, true},      // bare name
		{"docs/a.tmp", false, true},       // docs/*.tmp anchored path
		{"docs/sub/a.tmp", false, false},  // * does not cross "/"
		{"src/main.go", false, false},     // unmatched
	}
	for _, c := range cases {
		got, _ := gi.match(c.rel, c.isDir)
		if got != c.ignored {
			t.Errorf("match(%q, dir=%v) = %v, want %v", c.rel, c.isDir, got, c.ignored)
		}
	}
}

func TestIgnoreStackDefaultsAndNesting(t *testing.T) {
	var s ignoreStack
	s.push("", parseGitignore([]byte("*.log\n")))
	s.push("pkg", parseGitignore([]byte("!important.log\n")))

	if !s.ignored("node_modules", true) {
		t.Error("default dir node_modules should be ignored")
	}
	if !s.ignored(".DS_Store", false) {
		t.Error("default junk file should be ignored")
	}
	if !s.ignored("a.log", false) {
		t.Error("root *.log should ignore a.log")
	}
	// A deeper .gitignore re-includes important.log under pkg/.
	if s.ignored("pkg/important.log", false) {
		t.Error("nested !important.log should re-include")
	}
	// Outside pkg/, the negation does not apply.
	if !s.ignored("other/important.log", false) {
		t.Error("negation should be scoped to pkg/")
	}

	// Leaving pkg/ drops its frame so siblings don't inherit it.
	s.truncateTo("lib")
	if !s.ignored("pkg/important.log", false) {
		t.Error("after truncate, pkg negation should no longer apply")
	}
}

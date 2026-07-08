package core

// gentests.go is the :tests flow, the bridge between :review (opinion) and a
// future :verify (evidence): it turns the last review's findings into
// REPRODUCTION TEST FILES, so each claimed defect becomes checkable instead of
// a matter of the model's opinion. A finding whose test fails on the current
// code was real; a finding that can't be encoded as a test was probably noise.
//
// dcode stays read-only: the generated files are saved as a note under
// reviews/ (in the writable notes root) with clear target paths, for the user
// to copy into the repo and run — dcode itself never writes to the source tree
// nor executes anything.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"dcode/internal/tutor"
	"dcode/internal/vault"
)

const testsSystemPrompt = `You are writing REPRODUCTION TESTS for code-review findings, so each claimed
defect becomes checkable instead of a matter of opinion.
For each finding that is testable, produce a complete test file:
- First a line "File: <repo-relative path>" naming where the test belongs —
  next to the code under test, following the project's test naming convention
  (foo_test.go, test_foo.py, foo.test.ts, …).
- Then the FULL file contents in a fenced code block: imports, package/module
  declaration, everything needed to run it as-is.
- Each test must FAIL on the current code because of the defect and pass once
  it is fixed. Name the test after the finding it reproduces and add a short
  comment stating the expected failure.
- Use only real packages, symbols, and APIs from the diff and context — do NOT
  invent helpers or fixtures that don't exist. Use the language's standard test
  framework.
Skip findings that can't be tested in isolation, saying why in one line each.
Markdown.`

// TestsStream streams reproduction tests for review findings into onDelta and
// returns the full text. rev is the reviewed range (same syntax as :review);
// findings is the review's text, whose claims the tests encode. It errors when
// the range no longer has changes, since tests must target real code.
func (s *Service) TestsStream(ctx context.Context, rev, findings string, onDelta func(string)) (string, error) {
	args := DiffRange(rev)
	diff, err := s.gitDiff(ctx, args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("no changes to test (git diff %s was empty)", strings.Join(args, " "))
	}
	mapCtx := s.diffContext(changedPaths(diff))
	diff = clampChars(diff, maxDiffChars)

	var b strings.Builder
	b.WriteString("Review findings to reproduce as tests:\n\n")
	b.WriteString(clampChars(strings.TrimSpace(findings), maxDiffChars/2))
	if mapCtx != "" {
		b.WriteString("\n\nRepository map — the changed files' signatures and their callers:\n\n")
		b.WriteString(mapCtx)
	}
	b.WriteString("\n\nThe change under test:\n\n```diff\n" + diff + "\n```")
	hist := []tutor.ChatTurn{{Role: "user", Content: b.String()}}
	return s.tutor.StreamConversation(ctx, testsSystemPrompt, hist, onDelta)
}

// stagedFile is one reproduction test parsed out of the model's output: the
// repo-relative path it targets and the full file contents.
type stagedFile struct {
	Path string
	Body string
}

// testFileRE matches the "File: <path>" header lines the tests prompt asks for,
// tolerating markdown emphasis and backticks around the path.
var testFileRE = regexp.MustCompile("(?i)^[*_ \t]*File:?[*_ \t]*`?([^`\r\n]+?)`?[*_ \t]*$")

// parseTestFiles extracts the generated test files from the model's markdown:
// each "File: <path>" header followed by a fenced code block becomes one file.
// Paths are sanitized — absolute paths and ".." escapes are dropped — because
// they originate from model output.
func parseTestFiles(text string) []stagedFile {
	var out []stagedFile
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		m := testFileRE.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if m == nil {
			continue
		}
		p := filepath.ToSlash(strings.TrimSpace(m[1]))
		if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			continue // never let model output escape the staging dir
		}
		// Find the code fence that follows (allowing a blank/prose line between).
		j := i + 1
		for j < len(lines) && j <= i+3 && !strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
			j++
		}
		if j >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
			continue
		}
		var body []string
		k := j + 1
		for ; k < len(lines) && strings.TrimSpace(lines[k]) != "```"; k++ {
			body = append(body, lines[k])
		}
		if len(body) > 0 {
			out = append(out, stagedFile{Path: p, Body: strings.Join(body, "\n") + "\n"})
		}
		i = k
	}
	return out
}

// stageTestFiles writes the parsed test files as REAL files under the writable
// notes root — reproduction/<slug>/<path> — mirroring where each belongs in the
// repo, so one `cp -R` drops them all in place. Returns the absolute staging
// dir and the staged relative paths.
func (s *Service) stageTestFiles(slug string, files []stagedFile) (string, []string, error) {
	if s.notes == nil || len(files) == 0 {
		return "", nil, nil
	}
	dir := filepath.Join(s.notes.Root(), "reproduction", slug)
	var staged []string
	for _, f := range files {
		abs := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return dir, staged, err
		}
		if err := os.WriteFile(abs, []byte(f.Body), 0o644); err != nil {
			return dir, staged, err
		}
		staged = append(staged, f.Path)
	}
	return dir, staged, nil
}

// SaveGeneratedTests materializes the generated reproduction tests as real
// files under the notes root (reproduction/<slug>/…, mirroring their target
// paths in the repo) and saves the full output as a note beside the review
// ("reviews/<slug>-tests.md") with copy-and-run instructions. dcode never
// touches the source tree itself — the user copies the staged files in.
func (s *Service) SaveGeneratedTests(rev, text string) (NoteMeta, error) {
	label := strings.TrimSpace(rev)
	if label == "" {
		label = "working-changes"
	}
	slug := vault.Slug(label)

	stageDir, staged, err := s.stageTestFiles(slug, parseTestFiles(text))
	if err != nil {
		return NoteMeta{}, fmt.Errorf("staging test files: %w", err)
	}
	var use strings.Builder
	if len(staged) > 0 {
		use.WriteString("**Staged as real files** under `" + stageDir + "`:\n\n")
		for _, p := range staged {
			use.WriteString("- `" + p + "`\n")
		}
		use.WriteString("\nCopy them into the repo, run your tests, delete them after:\n\n")
		use.WriteString("```sh\ncp -R \"" + stageDir + "/\"* \"" + s.ProjectRoot() + "/\"\n```\n")
		use.WriteString("\nA test that FAILS confirms its finding is real; one that passes\nmeans the finding was likely noise. Then `:verify` runs the suite.\n")
	} else {
		use.WriteString("**How to use:** copy each file below to its stated path in your repo, run your\n" +
			"test runner, and delete the files when done. A test that FAILS confirms its\n" +
			"finding is real; a test that passes means the finding was likely noise.\n")
	}

	body := "# Reproduction Tests — " + label + "\n\n" +
		"_Generated by d-code from the `:review` of `git diff " + strings.TrimSpace(rev) + "`._\n\n" +
		use.String() + "\n" +
		strings.TrimSpace(text) + "\n"
	return s.SaveNote("reviews/"+slug+"-tests.md", body)
}

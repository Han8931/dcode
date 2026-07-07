package core

// diff.go is the :diff flow: explaining a code change. It runs `git diff` in the
// project root and asks the model to explain WHAT changed and WHY — the
// highest-frequency real-world code-understanding task. The diff is read-only;
// d-code never writes to the user's repo.

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"dcode/internal/tutor"
	"dcode/internal/vault"
)

const diffSystemPrompt = `You are explaining a code change to a reviewer.
Given a unified git diff, explain WHAT changed and WHY it likely changed:
- Intent: a 1-2 sentence summary of the change as a whole.
- Walkthrough: the significant hunks, grouped by file, in plain language.
- Risks: behavioral changes, edge cases, or anything a reviewer should double-check.
Reference real file names and symbols from the diff. Markdown. Do NOT invent
changes that are not present in the diff.`

// maxDiffChars bounds the diff sent to the model so a sprawling change still
// fits a small model's context window. maxDiffMapChars bounds the structural
// map prepended before it — kept compact, since the diff itself is the star.
const (
	maxDiffChars    = 24000
	maxDiffMapChars = 4000
)

// diffFileRE pulls the changed file paths out of a unified diff's "diff --git
// a/… b/…" headers; the "b/" path is the post-change name.
var diffFileRE = regexp.MustCompile(`(?m)^diff --git a/\S+ b/(\S+)`)

// changedPaths returns the set of files a unified diff touches.
func changedPaths(diff string) map[string]bool {
	set := map[string]bool{}
	for _, m := range diffFileRE.FindAllStringSubmatch(diff, -1) {
		set[m[1]] = true
	}
	return set
}

// diffContext builds a focused structural map for a diff: the signatures of the
// changed files plus every file that references a symbol they define — the
// change's structural neighbourhood — so the model can judge WHAT changed
// against how the code is actually wired, not the diff text alone. "" when there
// is nothing to add (no changed source files, or the tree can't be listed).
func (s *Service) diffContext(changed map[string]bool) string {
	if len(changed) == 0 {
		return ""
	}
	files, err := s.code.List()
	if err != nil {
		return ""
	}
	m := buildRepoMap(files)
	return clampChars(renderRepoMap(m, focusFiles(m, files, changed), maxDiffMapChars), maxDiffMapChars)
}

// gitDiff runs `git diff <args>` in the project root and returns its output.
// A non-zero exit (not a git repo, bad revision) is surfaced with git's message.
func (s *Service) gitDiff(ctx context.Context, args []string) (string, error) {
	cmdArgs := append([]string{"-C", s.ProjectRoot(), "--no-pager", "diff"}, args...)
	out, err := exec.CommandContext(ctx, "git", cmdArgs...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git diff failed: %s", msg)
	}
	return string(out), nil
}

// DiffRange normalizes a :diff argument into git-diff args: empty means the
// working changes against HEAD; anything else is passed through verbatim (so
// ":diff main", ":diff HEAD~1", or ":diff A B" all work).
func DiffRange(rev string) []string {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		return []string{"HEAD"}
	}
	return strings.Fields(rev)
}

// DiffStream streams an explanation of a git diff into onDelta and returns the
// full text. rev selects the range (see DiffRange). It errors if the range is
// empty (nothing changed) so the UI can say so rather than calling the model
// with no diff. Offline, StreamConversation returns its own canned message.
func (s *Service) DiffStream(ctx context.Context, rev string, onDelta func(string)) (string, error) {
	args := DiffRange(rev)
	diff, err := s.gitDiff(ctx, args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("no changes to explain (git diff %s was empty)", strings.Join(args, " "))
	}
	mapCtx := s.diffContext(changedPaths(diff))
	diff = clampChars(diff, maxDiffChars)
	user := "Explain this git diff:\n\n```diff\n" + diff + "\n```"
	if mapCtx != "" {
		user = "Repository map — structural context for the changed code " +
			"(signatures of the changed files and the files that reference them):\n\n" +
			mapCtx + "\n\n" + user
	}
	hist := []tutor.ChatTurn{{Role: "user", Content: user}}
	return s.tutor.StreamConversation(ctx, diffSystemPrompt, hist, onDelta)
}

// SaveDiffExplanation saves a change explanation as a note under "diffs/".
func (s *Service) SaveDiffExplanation(rev, text string) (NoteMeta, error) {
	label := strings.TrimSpace(rev)
	if label == "" {
		label = "working-changes"
	}
	body := "# Change Explanation — " + label + "\n\n" +
		"_Decoded by d-code from `git diff " + strings.TrimSpace(rev) + "`._\n\n" +
		strings.TrimSpace(text) + "\n"
	return s.SaveNote("diffs/"+vault.Slug(label)+".md", body)
}

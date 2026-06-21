package core

// diff.go is the :diff flow: explaining a code change. It runs `git diff` in the
// project root and asks the model to explain WHAT changed and WHY — the
// highest-frequency real-world code-understanding task. The diff is read-only;
// d-code never writes to the user's repo.

import (
	"context"
	"fmt"
	"os/exec"
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
// fits a small model's context window.
const maxDiffChars = 24000

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
	diff = clampChars(diff, maxDiffChars)
	hist := []tutor.ChatTurn{{Role: "user", Content: "Explain this git diff:\n\n```diff\n" + diff + "\n```"}}
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

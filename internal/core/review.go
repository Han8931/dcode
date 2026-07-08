package core

// review.go is the :review flow — dcode's first step from explaining code to
// VERIFYING it. Where :diff asks "what changed and why", :review is the
// adversarial twin: "what is wrong or risky in this change?". It feeds the model
// the same diff and focused repo-map context :diff uses (the changed files plus
// their callers), but with a defect-hunting prompt, and saves the findings as a
// note under reviews/ so a review is a durable artifact, not chat scroll.
//
// Like everything in dcode it is read-only: it inspects the diff and the source
// tree, and never executes or modifies project code.

import (
	"context"
	"fmt"
	"strings"

	"dcode/internal/tutor"
	"dcode/internal/vault"
)

const reviewSystemPrompt = `You are a meticulous senior code reviewer hunting for DEFECTS in a change.
Given a unified git diff (with structural context about the changed code and its
callers), report what is WRONG or RISKY — not what the change does:
- Verdict: one line up front — the main concern, or "looks safe" if it does.
- Findings: numbered, most severe first. For each give:
  **[high|medium|low] file:line** — the defect in one sentence, then a concrete
  failure scenario (what input or state makes it misbehave, and how), then a
  suggested fix in one or two sentences.
- Check especially: broken edge cases, off-by-one and boundary errors, nil/empty
  handling, error paths, concurrency hazards, behavior changes that break the
  CALLERS shown in the structural context, and security-sensitive handling.
Ground every finding in the diff or the provided context — do NOT invent code,
behavior, or files. If the change genuinely looks correct, say so briefly and
stop; do not pad with trivia or style nits. Markdown.`

// ReviewStream streams an adversarial review of a git diff into onDelta and
// returns the full text. rev selects the range exactly like :diff (empty means
// the working changes against HEAD). It errors when the range has no changes,
// so the UI can say so rather than reviewing nothing.
func (s *Service) ReviewStream(ctx context.Context, rev string, onDelta func(string)) (string, error) {
	args := DiffRange(rev)
	diff, err := s.gitDiff(ctx, args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diff) == "" {
		return "", fmt.Errorf("no changes to review (git diff %s was empty)", strings.Join(args, " "))
	}
	// Same structural grounding as :diff: the changed files' signatures plus the
	// files that reference them — the code a defect would break.
	mapCtx := s.diffContext(changedPaths(diff))
	diff = clampChars(diff, maxDiffChars)
	user := "Review this git diff for defects:\n\n```diff\n" + diff + "\n```"
	if mapCtx != "" {
		user = "Repository map — the changed files' signatures and the files that " +
			"reference them (the code this change could break):\n\n" +
			mapCtx + "\n\n" + user
	}
	hist := []tutor.ChatTurn{{Role: "user", Content: user}}
	return s.tutor.StreamConversation(ctx, reviewSystemPrompt, hist, onDelta)
}

// SaveReview saves review findings as a note under "reviews/", named for the
// reviewed range (or "working-changes" for the default review of HEAD).
func (s *Service) SaveReview(rev, text string) (NoteMeta, error) {
	label := strings.TrimSpace(rev)
	if label == "" {
		label = "working-changes"
	}
	body := "# Code Review — " + label + "\n\n" +
		"_Reviewed by d-code from `git diff " + strings.TrimSpace(rev) + "`._\n\n" +
		strings.TrimSpace(text) + "\n"
	return s.SaveNote("reviews/"+vault.Slug(label)+".md", body)
}

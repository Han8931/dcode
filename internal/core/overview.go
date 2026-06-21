package core

// overview.go is the :overview flow: a top-down architecture summary of the
// whole project, for onboarding to an unfamiliar codebase. It feeds the model a
// digest of the project — the source-file map plus the heads of the shallowest
// (usually entry-point) files — and streams back a structured overview that can
// be saved as a top-level note.

import (
	"context"
	"sort"
	"strings"

	"dcode/internal/tutor"
	"dcode/internal/vault"
)

const overviewSystemPrompt = `You are a senior engineer onboarding a developer to an unfamiliar codebase.
From the project map and key files provided, write a clear architecture OVERVIEW in Markdown.
Cover, in this order:
- Purpose: what the project is and does, in 1-2 sentences.
- Components: the main packages/modules and what each is responsible for.
- How it fits together: the primary flow or data path through the system.
- Entry points: where execution starts and the key files to read first.
Be concrete and reference real file and package names from what you are given.
Use short sections and bullet lists. Do NOT invent files, packages, or behavior
that the provided material does not support.`

const (
	maxOverviewFileList = 400   // source paths listed in the project map
	maxOverviewKeyFiles = 12    // files whose heads are inlined as "key files"
	maxOverviewKeyHead  = 60    // lines of each key file included
	maxOverviewCtxChars = 14000 // total digest budget
)

// OverviewStream streams an architecture overview of the whole project into
// onDelta and returns the full text. Offline, StreamConversation returns its
// own canned message.
func (s *Service) OverviewStream(ctx context.Context, onDelta func(string)) (string, error) {
	hist := []tutor.ChatTurn{{Role: "user", Content: "Project to summarize:\n\n" + s.projectDigest()}}
	return s.tutor.StreamConversation(ctx, overviewSystemPrompt, hist, onDelta)
}

// projectDigest builds the model's view of the whole project: the full source
// map (paths, capped) followed by the heads of the shallowest files, which are
// the likeliest entry points and top-level packages.
func (s *Service) projectDigest() string {
	files, err := s.code.List()
	if err != nil {
		return ""
	}
	var src []vault.Note
	for _, f := range files {
		if vault.IsSource(f.RelPath) {
			src = append(src, f)
		}
	}

	var b strings.Builder
	b.WriteString("Project: " + s.ProjectName() + "\n\nSource files:\n")
	for i, f := range src {
		if i >= maxOverviewFileList {
			b.WriteString("  … and " + itoa(len(src)-maxOverviewFileList) + " more\n")
			break
		}
		b.WriteString("  " + f.RelPath + "\n")
	}

	// Key files: shallowest path depth first (entry points / package roots), then
	// alphabetical, so the digest leads with the files worth reading first.
	ranked := append([]vault.Note(nil), src...)
	sort.SliceStable(ranked, func(i, j int) bool {
		di, dj := strings.Count(ranked[i].RelPath, "/"), strings.Count(ranked[j].RelPath, "/")
		if di != dj {
			return di < dj
		}
		return ranked[i].RelPath < ranked[j].RelPath
	})
	b.WriteString("\nKey files (heads):\n")
	for i, f := range ranked {
		if i >= maxOverviewKeyFiles || b.Len() >= maxOverviewCtxChars {
			break
		}
		b.WriteString("\n--- " + f.RelPath + " ---\n")
		b.WriteString(headLines(f.Body, maxOverviewKeyHead))
		b.WriteString("\n")
	}
	return clampChars(strings.TrimSpace(b.String()), maxOverviewCtxChars)
}

// SaveOverview writes an architecture overview as a top-level project note.
func (s *Service) SaveOverview(text string) (NoteMeta, error) {
	body := "# Architecture Overview — " + s.ProjectName() + "\n\n" +
		"_Decoded by d-code._\n\n" + strings.TrimSpace(text) + "\n"
	return s.SaveNote("OVERVIEW.md", body)
}

package tutor

// General (subject-agnostic) tutor capabilities: generating a lesson as a
// markdown NOTE, and — d-code's headline flow — explaining a piece of code,
// grounded in its file and related project files.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// explainSystemPrompt frames the model as a code explainer. It must ground its
// explanation in the provided code and context, reference real identifiers, and
// reply in Markdown prose (not a rewrite of the code).
const explainSystemPrompt = `You are an expert software engineer explaining code to another developer who is reading this codebase.
Explain the given code clearly and concretely:
- what it does and why it exists (its purpose/role);
- how it is structured — the key functions, types, and variables, and what each is for;
- the important control flow and data flow;
- notable edge cases, assumptions, gotchas, or pitfalls;
- how it fits together with the surrounding file and the related project files provided as context.
Reference the real identifiers from the code. Prefer understanding over restating: do NOT paraphrase the
code line by line, and do NOT echo the whole file back. Reply in clear Markdown — short prose with a few
headings or bullet points. If a selection is given, focus on it but use the surrounding file and project
context to explain how it connects to the rest.`

// ExplainStream streams an explanation of code, grounded in its file and related
// project files. focus, when non-empty, is a selected excerpt to center on; code
// is the whole file (surrounding context); projectContext is a digest of related
// files. onDelta receives each chunk; the full text is returned.
func (t *Tutor) ExplainStream(ctx context.Context, lang, focus, code, projectContext string, onDelta func(string)) (string, error) {
	if t.offline {
		s := "I'm offline right now (no AI provider configured), so I can't generate an explanation. " +
			"Set OPENAI_API_KEY, or point the config at a local Ollama model, then :explain again."
		if onDelta != nil {
			onDelta(s)
		}
		return s, nil
	}
	if lang == "" {
		lang = "text"
	}

	var b strings.Builder
	if strings.TrimSpace(focus) != "" {
		b.WriteString("Explain this selected " + lang + " code:\n```" + lang + "\n" + focus + "\n```\n\n")
		if strings.TrimSpace(code) != "" {
			b.WriteString("It belongs to this file (surrounding context):\n```" + lang + "\n" + code + "\n```\n")
		}
	} else {
		b.WriteString("Explain this " + lang + " file:\n```" + lang + "\n" + code + "\n```\n")
	}
	if strings.TrimSpace(projectContext) != "" {
		b.WriteString("\nRelated files from the same project, for cross-file context " +
			"(may be truncated — use them to explain how things connect):\n" + projectContext + "\n")
	}

	hist := []ChatTurn{{Role: "user", Content: b.String()}}
	return t.StreamConversation(ctx, explainSystemPrompt, hist, onDelta)
}

// NoteContent is an AI-generated lesson shaped for the vault: structured
// metadata plus a markdown body the learner can keep, edit, and link from.
type NoteContent struct {
	Title   string   `json:"title"`
	Subject string   `json:"subject"`
	Tags    []string `json:"tags"`
	Body    string   `json:"body"` // markdown
}

// GenerateNote turns "I want to learn X" into a self-contained lesson note on
// any subject (languages, math, history, …) — not just programming. The body is
// markdown and may wrap key prerequisite concepts in [[wikilinks]] so the
// learner can branch out into linked notes.
func (t *Tutor) GenerateNote(ctx context.Context, request string) (NoteContent, error) {
	if t.offline {
		return offlineNote(request), nil
	}

	system := `You are a knowledgeable tutor creating a focused, self-contained lesson NOTE
for a self-directed learner. The subject can be anything (a language, math, history,
science, music, …), not only programming.
Respond with ONLY a JSON object, no prose, no markdown fences, matching:
{
  "title": "a short, specific note title",
  "subject": "the broad subject area, lowercase (e.g. \"math\", \"spanish\", \"history\")",
  "tags": ["2-4", "short", "kebab-case", "tags"],
  "body": "the lesson as MARKDOWN: a few short sections of clear prose, optionally one worked example. Wrap key prerequisite or related concepts in [[wikilinks]] so the learner can branch into linked notes. Do not restate the title as an H1 — start with the explanation."
}` + t.levelClause()

	raw, err := t.chat(ctx, system, "Teach me about: "+request)
	if err != nil {
		return NoteContent{}, err
	}
	nc, err := parseNoteContent(raw)
	if err != nil {
		return NoteContent{}, fmt.Errorf("could not parse generated note: %w", err)
	}
	if strings.TrimSpace(nc.Title) == "" {
		nc.Title = strings.TrimSpace(request)
	}
	if strings.TrimSpace(nc.Body) == "" {
		nc.Body = strings.TrimSpace(raw)
	}
	return nc, nil
}

// --- parsing helpers ---

func parseNoteContent(raw string) (NoteContent, error) {
	s, ok := extractJSONObject(raw)
	if !ok {
		return NoteContent{}, fmt.Errorf("no JSON object found")
	}
	var nc NoteContent
	if err := json.Unmarshal([]byte(s), &nc); err != nil {
		return NoteContent{}, err
	}
	return nc, nil
}

// extractJSONObject pulls the first {...} JSON object out of a model reply,
// tolerating markdown fences around it (shared shape with parseChallenge).
func extractJSONObject(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return "", false
	}
	return s[start : end+1], true
}

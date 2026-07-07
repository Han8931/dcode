package tutor

// Code-explanation capabilities: explain a piece of code, grounded in its file
// and related project files.

import (
	"context"
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

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// shell.go holds the small set of package-level types and helpers that used to
// live in older TUI flows but are still shared by the vault
// front-end: the pane identifiers, the run-outcome type, and a couple of tiny
// render helpers.

// pane identifies one of the three TUI panes.
type pane int

const (
	paneSidebar pane = iota
	paneEditor
	paneChat
)

// editorBiasStep is how many points :compact / :wide shift the editor/chat
// split each press; editorBiasMax bounds the accumulated shift.
const (
	editorBiasStep = 8
	editorBiasMax  = 40
)

// clampMin returns v, but never below lo.
func clampMin(v, lo int) int { return max(v, lo) }

// clampRange clamps v into [lo, hi].
func clampRange(v, lo, hi int) int { return min(max(v, lo), hi) }

// firstLine returns the first line of s (without its trailing newline).
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// langForPath picks the editor's syntax-highlight language for a file. go,
// python, and markdown have full rules; any other source file gets generic
// highlighting (strings/numbers/comments color in any syntax); non-source,
// non-markdown files render as plain text.
func langForPath(p string, isSource bool) string {
	switch strings.ToLower(p[strings.LastIndexByte(p, '.')+1:]) {
	case "md", "markdown":
		return "markdown"
	case "go":
		return "go"
	case "py":
		return "python"
	}
	if isSource {
		return "code" // unknown source language -> generic highlighter
	}
	return "text"
}

// SwitchTarget tells RunVault's caller what to do when the TUI exits. d-code has
// a single mode, so the only outcome is to quit.
type SwitchTarget int

const (
	StayQuit SwitchTarget = iota
)

// Outcome is returned by RunVault.
type Outcome struct {
	Target SwitchTarget
}

// bold renders s in bold — a shared helper for the sidebar and chat panes.
func bold(s string) string { return lipgloss.NewStyle().Bold(true).Render(s) }

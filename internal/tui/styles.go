package tui

import (
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// noticeTTL is how long a status-bar notice stays visible.
const noticeTTL = 5 * time.Second

// Pane border styles. The focused pane gets a bright border; others are dim.
// We switch only BorderForeground (not the border runes) so box widths stay
// stable across focus changes and the layout never jumps.
var (
	focusedBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39"))

	blurredBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	titleBar = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("25")).
			Padding(0, 1)

	statusBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	editorHeader = lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("238")).
			Padding(0, 1)

	hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)

	// checkButtonText / checkButton render the clickable "run the tests" control
	// in the title bar. Clicking it is equivalent to Ctrl-S / :submit; its screen
	// bounds (for hit-testing the click) come from Model.checkButtonBounds.
	checkButtonText = " ▸ Check answer "
	checkButton     = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("232")).
			Background(lipgloss.Color("79"))

	// Sidebar row styles and colors. selectedBg paints the cursor bar when the
	// pane is focused; selectedBlurredBg is the dimmed bar shown when the pane has
	// lost focus (the way ranger/lf fade an inactive pane's selection). doneColor
	// and wipColor are reused both for the standalone glyph styles and for the
	// glyphs painted onto the selection bar.
	selectedBg        = lipgloss.Color("24")
	selectedBlurredBg = lipgloss.Color("238")
	selectedFg        = lipgloss.Color("231")
	doneColor         = lipgloss.Color("42")
	wipColor          = lipgloss.Color("214")

	selectedRow = lipgloss.NewStyle().Foreground(selectedFg).Background(selectedBg)
	headerRow   = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("238")).Bold(true)
	doneGlyph   = lipgloss.NewStyle().Foreground(doneColor)
	wipGlyph    = lipgloss.NewStyle().Foreground(wipColor)

	// activeItem bolds the sidebar row open in the editor (replacing the old
	// "▶" marker column); markedItem tints rows space-marked for a batch op.
	activeItem = lipgloss.NewStyle().Bold(true)
	markedItem = lipgloss.NewStyle().Foreground(wipColor)

	// Chat transcript styles. Each speaker turn opens with a colored BADGE on
	// its own line (" you " / " tutor " / " lesson " on a filled background) so
	// who is talking is unmistakable even when skimming; the message body stays
	// a calm, high-contrast neutral so long passages read comfortably.
	chatBodyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	chatUserBadge   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("81"))  // learner — cyan-blue
	chatTutorBadge  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("79"))  // tutor — teal
	chatLessonBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("222")) // lesson — warm gold
	chatQuizBadge   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("215")) // quiz — peach

	// chatBusyStyle renders the in-pane "⠹ tutor thinking…" progress line.
	chatBusyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("79")).Italic(true)

	// Input-area styles. A dim rule (chatInputRule) separates the transcript
	// from the typing area, the whole typing area sits on a soft grey wash
	// (chatInputBG) so it reads as a distinct field, and the "> " prompt prefix
	// is painted bright cyan when the pane is focused (matching the "you" badge)
	// and dim when it isn't, so it's always obvious where typing happens.
	//
	// chatInputBGSeq is the raw SGR that opens chatInputBG. The textarea sprays
	// reset codes (\e[0m) mid-line, each of which would drop the background, so
	// inputView re-asserts this sequence after every reset to keep the wash
	// solid across the full width (see chat.go inputView).
	chatInputBG     = lipgloss.Color("237")
	chatInputBGSeq  = "\x1b[48;5;237m"
	chatInputRule   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	chatPromptFocus = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	chatPromptBlur  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	// chatPromptNormal marks the input's Vim Normal mode (Esc in the chat):
	// green, matching the editor's NORMAL badge.
	chatPromptNormal = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)

	// chatCodeGutter is the "│ " bar marking syntax-highlighted code blocks in
	// transcript messages. The tinted gutter and dark wash make code read as a
	// separate surface while the editor highlighter colors the tokens inside it.
	chatCodeGutter = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	chatCodeLine   = lipgloss.NewStyle().Background(lipgloss.Color("236"))

	chatSystemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)

	// chatSelStyle paints the mouse drag-selection over the transcript (the
	// sidebar's selection colors, so "selected" reads the same app-wide);
	// Alt-C copies the selected text.
	chatSelStyle = lipgloss.NewStyle().Foreground(selectedFg).Background(selectedBg)

	// noticeStyle renders transient command feedback in the status bar (copy
	// confirmations, resize/fold notices, unknown commands…).
	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("222"))

	// backlinkHeaderStyle titles the "↩ Linked mentions" panel under the editor.
	backlinkHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("79")).Bold(true)
	chatOkStyle         = lipgloss.NewStyle().Foreground(doneColor).Bold(true)             // match the sidebar "done" green
	chatFailStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("210")).Bold(true) // soft red, easier on the eyes than 203
)

// borderStyle returns the focused or blurred border depending on whether the
// pane is the active one.
func borderStyle(active bool) lipgloss.Style {
	if active {
		return focusedBorder
	}
	return blurredBorder
}

func enableTUIColor() {
	normalizeRuneWidth()
	if os.Getenv("NO_COLOR") != "" || os.Getenv("CLICOLOR") == "0" {
		return
	}
	lipgloss.SetColorProfile(termenv.ANSI256)
}

// normalizeRuneWidth pins go-runewidth to NARROW ambiguous-width characters,
// so the whole app measures glyph widths the one way the rest of the render
// stack already does. Under a CJK locale (LANG=ko_KR/ja_JP/zh_*) go-runewidth
// auto-enables East Asian Width, counting ambiguous characters — the arrows,
// ≤ ≥ · × ² and dashes that fill lessons and AI replies — as 2 cells. But the
// chat viewport, the textarea, and charm's x/ansi all measure them as 1 (via
// uniseg), as do modern terminals by default. That split is what corrupted the
// layout (misaligned borders, " ????" cell garbage) once scrolled into
// symbol-dense content. Forcing width 1 everywhere — lipgloss's word-wrap and
// the editor's soft-wrap both read this condition — keeps every pane in sync.
func normalizeRuneWidth() {
	runewidth.DefaultCondition.EastAsianWidth = false
}

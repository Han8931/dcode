package tui

import (
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"dcode/internal/theme"
)

// pal is the active palette; every style below derives from it, so the whole UI
// stays one coherent scheme (see internal/theme). applyThemeStyles rebuilds the
// styles from theme.Current — at init and again whenever :theme switches.
var pal = theme.Current

// noticeTTL is how long a status-bar notice stays visible.
const noticeTTL = 5 * time.Second

// checkButtonText / checkButton render the clickable "run the tests" control
// in the title bar. Clicking it is equivalent to Ctrl-S / :submit; its screen
// bounds (for hit-testing the click) come from Model.checkButtonBounds.
const checkButtonText = " ▸ Check answer "

// The UI's styles. Declared here, assigned by applyThemeStyles so a :theme
// switch can rebuild them all from the new palette at runtime.
var (
	focusedBorder, blurredBorder       lipgloss.Style
	titleBar, statusBar, editorHeader  lipgloss.Style
	hintStyle, errStyle, checkButton   lipgloss.Style
	selectedRow, headerRow             lipgloss.Style
	doneGlyph, wipGlyph                lipgloss.Style
	activeItem, markedItem             lipgloss.Style
	chatBodyStyle, chatBusyStyle       lipgloss.Style
	chatUserBadge, chatTutorBadge      lipgloss.Style
	chatQuizBadge                      lipgloss.Style
	chatInputRule, chatPromptFocus     lipgloss.Style
	chatPromptBlur, chatPromptNormal   lipgloss.Style
	chatCodeGutter, chatCodeLine       lipgloss.Style
	chatSystemStyle, chatSelStyle      lipgloss.Style
	noticeStyle, backlinkHeaderStyle   lipgloss.Style
	tabActive, tabInactive, tabBarFill lipgloss.Style
	chatOkStyle, chatFailStyle         lipgloss.Style
	selectedBg, selectedBlurredBg      lipgloss.Color
	selectedFg, doneColor, wipColor    lipgloss.Color
	chatInputBG                        lipgloss.Color
	chatInputBGSeq                     string
)

func init() { applyThemeStyles() }

// applyThemeStyles (re)builds every tui style from the active theme palette.
// Called at package init and again by :theme after theme.Set, so a switch takes
// effect on the next render without restarting.
func applyThemeStyles() {
	pal = theme.Current

	// Pane borders: the focused pane gets a bright accent border; others a muted
	// surface tone. Only BorderForeground changes (not the border runes) so box
	// widths stay stable across focus changes and the layout never jumps.
	focusedBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Blue)
	blurredBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Surface1)

	titleBar = lipgloss.NewStyle().
		Bold(true).
		Foreground(pal.Text).
		Background(pal.Surface0).
		Padding(0, 1)
	statusBar = lipgloss.NewStyle().
		Foreground(pal.Subtext).
		Background(pal.Mantle).
		Padding(0, 1)
	editorHeader = lipgloss.NewStyle().
		Foreground(pal.Text).
		Background(pal.Surface0).
		Padding(0, 1)

	hintStyle = lipgloss.NewStyle().Foreground(pal.Overlay)
	errStyle = lipgloss.NewStyle().Foreground(pal.Red).Bold(true)
	checkButton = lipgloss.NewStyle().Bold(true).
		Foreground(pal.Crust).
		Background(pal.Green)

	// Sidebar rows: selectedBg paints the cursor bar when the pane is focused;
	// selectedBlurredBg is the faded bar when it isn't (ranger/lf style). done/
	// wip colors are shared by the glyph styles and the selection bar painter.
	selectedBg = pal.Surface1
	selectedBlurredBg = pal.Surface0
	selectedFg = pal.Text
	doneColor = pal.Green
	wipColor = pal.Peach

	selectedRow = lipgloss.NewStyle().Foreground(selectedFg).Background(selectedBg)
	headerRow = lipgloss.NewStyle().Foreground(pal.Subtext).Background(pal.Surface0).Bold(true)
	doneGlyph = lipgloss.NewStyle().Foreground(doneColor)
	wipGlyph = lipgloss.NewStyle().Foreground(wipColor)

	// activeItem bolds the sidebar row open in the editor; markedItem tints rows
	// space-marked for a batch op.
	activeItem = lipgloss.NewStyle().Bold(true).Foreground(pal.Text)
	markedItem = lipgloss.NewStyle().Foreground(wipColor)

	// Chat transcript: each speaker turn opens with a colored badge (" you " /
	// " tutor ") so who is talking is unmistakable; bodies stay a calm neutral.
	chatBodyStyle = lipgloss.NewStyle().Foreground(pal.Text)
	chatUserBadge = lipgloss.NewStyle().Bold(true).Foreground(pal.Crust).Background(pal.Blue)  // user — blue
	chatTutorBadge = lipgloss.NewStyle().Bold(true).Foreground(pal.Crust).Background(pal.Teal) // tutor — teal
	chatQuizBadge = lipgloss.NewStyle().Bold(true).Foreground(pal.Crust).Background(pal.Peach) // quiz — peach
	chatBusyStyle = lipgloss.NewStyle().Foreground(pal.Teal).Italic(true)

	// Input area: a dim rule separates the transcript from the typing area, the
	// typing area sits on a soft surface wash, and the "> " prompt is bright
	// accent when focused / dim when not.
	//
	// chatInputBG is an ANSI-256 color because chatInputBGSeq is a RAW SGR the
	// textarea's mid-line resets (\e[0m) force chat.go inputView to re-assert;
	// a raw 256-color code renders everywhere, where raw truecolor would not.
	// It is derived from the palette's Surface0 so light themes get a light wash.
	chatInputBG, chatInputBGSeq = ansi256Bg(pal.Surface0)
	chatInputRule = lipgloss.NewStyle().Foreground(pal.Surface2)
	chatPromptFocus = lipgloss.NewStyle().Foreground(pal.Blue).Bold(true)
	chatPromptBlur = lipgloss.NewStyle().Foreground(pal.Overlay)
	// chatPromptNormal marks the input's Vim Normal mode (Esc in the chat):
	// green, matching the editor's NORMAL badge.
	chatPromptNormal = lipgloss.NewStyle().Foreground(pal.Green).Bold(true)

	// Code blocks in chat: a tinted "│ " gutter and dark wash make code read as
	// a separate surface; the editor highlighter colors the tokens inside.
	chatCodeGutter = lipgloss.NewStyle().Foreground(pal.Blue).Bold(true)
	chatCodeLine = lipgloss.NewStyle().Background(pal.Mantle)

	chatSystemStyle = lipgloss.NewStyle().Foreground(pal.Overlay).Italic(true)

	// chatSelStyle paints the mouse drag-selection over the transcript (the
	// sidebar's selection colors, so "selected" reads the same app-wide).
	chatSelStyle = lipgloss.NewStyle().Foreground(selectedFg).Background(selectedBg)

	// noticeStyle renders transient command feedback in the status bar.
	noticeStyle = lipgloss.NewStyle().Foreground(pal.Yellow)

	// Editor tab bar (NERDTree-style tabs): the active tab is a filled accent
	// chip; the others recede onto a raised surface; the strip sits on Mantle.
	tabActive = lipgloss.NewStyle().Bold(true).Foreground(pal.Crust).Background(pal.Blue)
	tabInactive = lipgloss.NewStyle().Foreground(pal.Subtext).Background(pal.Surface0)
	tabBarFill = lipgloss.NewStyle().Background(pal.Mantle)

	// backlinkHeaderStyle titles the "↩ Linked mentions" panel under the editor.
	backlinkHeaderStyle = lipgloss.NewStyle().Foreground(pal.Teal).Bold(true)
	chatOkStyle = lipgloss.NewStyle().Foreground(doneColor).Bold(true) // match the sidebar "done" green
	chatFailStyle = lipgloss.NewStyle().Foreground(pal.Maroon).Bold(true)
}

// ansi256Bg converts a palette color to its nearest ANSI-256 index, returning
// both the lipgloss color and the raw background SGR that chat.go re-asserts
// after the textarea's mid-line resets. Falls back to a neutral grey if the
// conversion yields no 256 index.
func ansi256Bg(c lipgloss.Color) (lipgloss.Color, string) {
	if a, ok := termenv.ANSI256.Convert(termenv.RGBColor(string(c))).(termenv.ANSI256Color); ok {
		n := strconv.Itoa(int(a))
		return lipgloss.Color(n), "\x1b[48;5;" + n + "m"
	}
	return lipgloss.Color("237"), "\x1b[48;5;237m"
}

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
		return // honor no-color; lipgloss/termenv render plain
	}
	// Render the truecolor palette natively where the terminal truly supports it;
	// otherwise floor at ANSI-256 (the prior robust behavior — many terminals
	// under-report, and lipgloss downsamples the hex palette cleanly to 256).
	if termenv.ColorProfile() == termenv.TrueColor {
		lipgloss.SetColorProfile(termenv.TrueColor)
		return
	}
	lipgloss.SetColorProfile(termenv.ANSI256)
}

// normalizeRuneWidth pins go-runewidth to NARROW ambiguous-width characters,
// so the whole app measures glyph widths the one way the rest of the render
// stack already does. Under a CJK locale (LANG=ko_KR/ja_JP/zh_*) go-runewidth
// auto-enables East Asian Width, counting ambiguous characters — the arrows,
// ≤ ≥ · × ² and dashes that fill AI replies — as 2 cells. But the
// chat viewport, the textarea, and charm's x/ansi all measure them as 1 (via
// uniseg), as do modern terminals by default. That split is what corrupted the
// layout (misaligned borders, " ????" cell garbage) once scrolled into
// symbol-dense content. Forcing width 1 everywhere — lipgloss's word-wrap and
// the editor's soft-wrap both read this condition — keeps every pane in sync.
func normalizeRuneWidth() {
	runewidth.DefaultCondition.EastAsianWidth = false
}

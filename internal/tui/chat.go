package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"dcode/internal/editor"
)

// chatRole tags a transcript block so it can be styled and (for some kinds)
// fed back to the AI as conversation history.
type chatRole int

const (
	roleSystem chatRole = iota // app notices: "— now on: … —", errors
	roleLesson                 // the lesson text
	roleUser                   // the learner's typed question
	roleTutor                  // the AI's reply / feedback
	roleQuiz                   // the current topic's quiz/task prompt
	roleOK                     // test pass line
	roleFail                   // test fail line
)

type chatBlock struct {
	role chatRole
	text string
}

// chatInputRows is the height of the typing area. Multi-row so longer
// questions wrap and stay fully visible while being written.
const chatInputRows = 3

// chatModel is the right pane: a scrollable transcript, an optional animated
// "working…" line while an AI call is in flight, and a multi-row input. The
// transcript is kept as structured blocks so it can be re-wrapped on resize.
type chatModel struct {
	vp      viewport.Model
	input   textarea.Model
	blocks  []chatBlock
	w, h    int
	focused bool
	// normal is the input's Vim Normal mode (Esc toggles in): motions and
	// edits work on the draft, ":" opens the parent's command line, i/a
	// return to typing. pendingOp holds the first key of dd/cc.
	normal    bool
	pendingOp rune

	// busy is the label of the in-flight async op ("" = idle); busyTick drives
	// the spinner animation, advanced by the parent's spinner tick.
	busy     string
	busyTick int

	// codeLang is the language assumed for UNLABELED ``` fences in tutor and
	// lesson messages (labeled fences always win). Empty renders them plain.
	codeLang string

	// Input history, recalled with the arrow keys (readline-style). histPos ==
	// len(inputHist) means "live" (composing a new message); draft stashes the
	// live input while navigating.
	inputHist []string
	histPos   int
	draft     string

	// Mouse drag-selection over the transcript (copied with Alt-C). Coordinates
	// are content-based — line index into contentLines and display-cell column —
	// so the selection survives scrolling. contentLines mirrors what reflow
	// loaded into the viewport.
	sel          chatSelection
	contentLines []string
}

// chatSelection is a span of transcript cells between the press anchor and the
// dragged head. active stays false until the mouse actually moves, so a plain
// click only clears the previous selection.
type chatSelection struct {
	active               bool
	anchorLine, anchorCol int
	headLine, headCol     int
}

// setCodeLang sets the default language for unlabeled code fences and
// re-renders the transcript with the new highlighting.
func (c *chatModel) setCodeLang(lang string) {
	lang = chatFenceLang(lang)
	if c.codeLang == lang {
		return
	}
	c.codeLang = lang
	c.reflow()
}

func newChat() chatModel {
	in := textarea.New()
	in.Placeholder = "ask the tutor…"
	// Show the "> " prompt only on the first line; blank the wrapped/extra rows
	// so the typing area reads as one prompt, not a column of them.
	in.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "> "
		}
		return "  "
	})
	in.FocusedStyle.Prompt = chatPromptFocus
	in.BlurredStyle.Prompt = chatPromptBlur
	// The default CursorLine style forces a black background, which would punch
	// a dark band through the grey wash on whichever line the cursor sits on.
	// Match it to the wash so the typing line blends in (see inputView).
	in.FocusedStyle.CursorLine = in.FocusedStyle.CursorLine.Background(chatInputBG)
	in.BlurredStyle.CursorLine = in.BlurredStyle.CursorLine.Background(chatInputBG)
	in.ShowLineNumbers = false
	in.CharLimit = 0
	in.SetHeight(chatInputRows)

	return chatModel{
		vp:    viewport.New(0, 0),
		input: in,
	}
}

// setSize lays the pane out within w×h: the input block at the bottom, an
// optional busy line above it, and the transcript in the remaining rows.
func (c *chatModel) setSize(w, h int) {
	c.w, c.h = w, h
	c.clearSelect() // a re-wrap moves every line; old coordinates are meaningless
	c.relayout()
	c.reflow()
}

// relayout recomputes the vertical split (transcript / busy line / input) from
// the stored size and current busy state.
func (c *chatModel) relayout() {
	if c.w <= 0 || c.h <= 0 {
		return
	}
	inputH := chatInputRows
	if c.h < 7 {
		inputH = 1 // tiny panes: give the transcript what little there is
	}
	c.input.SetWidth(c.w - 2) // room for the "> " prompt
	c.input.SetHeight(inputH)

	vpH := c.h - inputH - 1 // -1 for the separator rule above the input
	if c.busy != "" {
		vpH--
	}
	if vpH < 1 {
		vpH = 1
	}
	c.vp.Width = c.w
	c.vp.Height = vpH
}

// setBusy shows (or, with "", hides) the animated progress line. The label
// names the operation, e.g. "tutor thinking".
func (c *chatModel) setBusy(label string) {
	if c.busy == label {
		return
	}
	follow := c.vp.AtBottom()
	c.busy = label
	c.relayout()
	if follow {
		c.vp.GotoBottom()
	}
}

// tickBusy advances the spinner animation one frame.
func (c *chatModel) tickBusy() { c.busyTick++ }

var busyFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (c chatModel) busyLine() string {
	frame := busyFrames[c.busyTick%len(busyFrames)]
	line := chatBusyStyle.Render(frame + " " + c.busy + "…")
	return lipgloss.NewStyle().MaxWidth(c.w).Render(line)
}

// scrollBy moves the transcript by n lines (negative = up), for drag-scrolling.
func (c *chatModel) scrollBy(n int) {
	if n < 0 {
		c.vp.ScrollUp(-n)
	} else if n > 0 {
		c.vp.ScrollDown(n)
	}
}

// --- mouse drag-selection ---

// selPoint maps pane-local viewport coordinates to content coordinates,
// clamped to the transcript.
func (c chatModel) selPoint(x, y int) (line, col int) {
	if y < 0 {
		y = 0
	}
	if c.vp.Height > 0 && y >= c.vp.Height {
		y = c.vp.Height - 1
	}
	line = c.vp.YOffset + y
	if n := len(c.contentLines); line >= n {
		line = n - 1
	}
	if line < 0 {
		line = 0
	}
	if x < 0 {
		x = 0
	}
	return line, x
}

// startSelect anchors a selection at a left press (pane-local coordinates).
// The previous selection is dropped; nothing is selected until the drag moves.
func (c *chatModel) startSelect(x, y int) {
	line, col := c.selPoint(x, y)
	c.sel = chatSelection{anchorLine: line, anchorCol: col, headLine: line, headCol: col}
}

// dragSelect extends the selection to the dragged position. Dragging past the
// top/bottom edge scrolls the transcript along, terminal-style.
func (c *chatModel) dragSelect(x, y int) {
	if y < 0 {
		c.vp.ScrollUp(1)
	} else if y >= c.vp.Height {
		c.vp.ScrollDown(1)
	}
	c.sel.headLine, c.sel.headCol = c.selPoint(x, y)
	c.sel.active = true
}

func (c *chatModel) clearSelect() { c.sel = chatSelection{} }

// selBounds orders the selection's endpoints top-to-bottom, left-to-right.
func (c chatModel) selBounds() (l1, c1, l2, c2 int) {
	l1, c1 = c.sel.anchorLine, c.sel.anchorCol
	l2, c2 = c.sel.headLine, c.sel.headCol
	if l2 < l1 || (l1 == l2 && c2 < c1) {
		l1, c1, l2, c2 = l2, c2, l1, c1
	}
	return l1, c1, l2, c2
}

// selectionText extracts the selected cells as plain text (ANSI stripped,
// per-line trailing padding trimmed), the head cell included.
func (c chatModel) selectionText() (string, bool) {
	if !c.sel.active || len(c.contentLines) == 0 {
		return "", false
	}
	l1, c1, l2, c2 := c.selBounds()
	var parts []string
	for ln := l1; ln <= l2 && ln < len(c.contentLines); ln++ {
		row := c.contentLines[ln]
		w := ansi.StringWidth(row)
		lo, hi := 0, w
		if ln == l1 {
			lo = c1
		}
		if ln == l2 && c2+1 < hi {
			hi = c2 + 1
		}
		if lo >= hi {
			parts = append(parts, "")
			continue
		}
		parts = append(parts, strings.TrimRight(ansi.Strip(ansi.Cut(row, lo, hi)), " "))
	}
	text := strings.Join(parts, "\n")
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

// overlaySelection repaints the selected span of the rendered viewport rows in
// the selection color so the drag is visible while it happens.
func (c chatModel) overlaySelection(view string) string {
	if !c.sel.active {
		return view
	}
	l1, c1, l2, c2 := c.selBounds()
	rows := strings.Split(view, "\n")
	for r := range rows {
		abs := c.vp.YOffset + r
		if abs < l1 || abs > l2 {
			continue
		}
		row := rows[r]
		w := ansi.StringWidth(row)
		lo, hi := 0, w
		if abs == l1 {
			lo = c1
		}
		if abs == l2 && c2+1 < hi {
			hi = c2 + 1
		}
		if lo >= hi || lo >= w {
			continue
		}
		rows[r] = ansi.Cut(row, 0, lo) +
			chatSelStyle.Render(ansi.Strip(ansi.Cut(row, lo, hi))) +
			ansi.Cut(row, hi, w)
	}
	return strings.Join(rows, "\n")
}

// copySelection copies the drag-selected transcript text (Alt-C) and returns a
// status notice describing the outcome.
func copySelection(c *chatModel) string {
	text, ok := c.selectionText()
	if !ok {
		return "nothing selected — drag over the transcript first, then ⌥c"
	}
	if err := copyToClipboard(text); err != nil {
		return "✓ sent selection to the terminal clipboard (OSC 52) — native clipboard unavailable: " + err.Error()
	}
	return "✓ copied selection (" + itoa(len([]rune(text))) + " chars)"
}

// snapshot returns the transcript blocks so a parent can stash them away when
// the learner switches topics.
func (c *chatModel) snapshot() []chatBlock { return c.blocks }

// restore replaces the transcript (nil = start fresh) and jumps to its tail —
// used when switching back to a previously visited topic.
func (c *chatModel) restore(blocks []chatBlock) {
	c.blocks = blocks
	c.reflow()
	c.vp.GotoBottom()
}

// append adds a block to the transcript and re-wraps. It follows the tail only
// when the view was already pinned to the bottom, so a new message can't yank
// the reader away while they're scrolled up in the history.
func (c *chatModel) append(role chatRole, text string) {
	follow := c.vp.AtBottom()
	c.blocks = append(c.blocks, chatBlock{role: role, text: strings.TrimRight(text, "\n")})
	c.reflow()
	if follow {
		c.vp.GotoBottom()
	}
}

// reflow renders all blocks wrapped to the current width and loads them into
// the viewport. Called on resize and on every append.
func (c *chatModel) reflow() {
	if c.w <= 0 {
		return
	}
	var b strings.Builder
	for i, blk := range c.blocks {
		if i > 0 {
			b.WriteString("\n\n") // a blank line between blocks so turns don't run together
		}
		b.WriteString(c.renderBlock(blk))
	}
	c.contentLines = strings.Split(b.String(), "\n")
	c.vp.SetContent(b.String())
}

// renderBlock styles one transcript block and wraps it to the pane width.
// Speaker turns get a colored badge on its own line with the body below, so
// who is talking is obvious at a glance; status lines (pass/fail and app
// notices) are short and keep a single tint.
func (c chatModel) renderBlock(blk chatBlock) string {
	w := c.w
	switch blk.role {
	case roleUser:
		return chatUserBadge.Render(" you ") + "\n" + c.renderRichBody(blk.text)
	case roleTutor:
		return chatTutorBadge.Render(" tutor ") + "\n" + c.renderRichBody(blk.text)
	case roleLesson:
		return chatLessonBadge.Render(" lesson ") + "\n" + c.renderRichBody(blk.text)
	case roleQuiz:
		return chatQuizBadge.Render(" quiz ") + "\n" + c.renderRichBody(blk.text)
	case roleOK:
		return chatOkStyle.Width(w).Render(blk.text)
	case roleFail:
		return chatFailStyle.Width(w).Render(blk.text)
	default:
		return chatSystemStyle.Width(w).Render(blk.text)
	}
}

// renderRichBody renders a tutor/lesson body: prose is word-wrapped neutrally,
// and fenced ``` code blocks are syntax-highlighted (via the editor's
// highlighter) behind a gutter bar instead of being word-wrapped.
func (c chatModel) renderRichBody(text string) string {
	lines := strings.Split(text, "\n")
	var out, prose, code []string
	lang, fenceMarker := "", ""
	inCode := false

	flushProse := func() {
		if len(prose) > 0 {
			// Tutor/lesson prose is markdown: style headings, **bold**,
			// `code`, [[wikilinks]], bullets, and quotes before wrapping
			// (lipgloss wraps ANSI-aware, so the colors survive the reflow).
			md := editor.Highlight("markdown", strings.Join(prose, "\n"))
			out = append(out, chatBodyStyle.Width(c.w).Render(md))
			prose = nil
		}
	}
	// renderCodeRows hard-wraps highlighted code to the pane (ANSI-aware) so no
	// code is ever clipped or word-wrapped mid-identifier; every visual row
	// keeps the gutter bar.
	renderCodeRows := func(src []string, l string) {
		if l == "" {
			l = c.codeLang
		}
		// Inside a chat ``` fence the content IS code by construction, so an
		// unlabeled fence (or a prose topic language like "essay") still gets
		// the language-agnostic highlighting — strings, numbers, comments.
		switch l {
		case "", "plain", "text", "essay", "physics":
			l = "code" // any non-prose tag routes to the generic rules
		}
		width := c.w - 2 // room for the "│ " gutter
		if width < 4 {
			width = 4
		}
		hl := editor.Highlight(l, strings.Join(src, "\n"))
		var rows []string
		for _, row := range strings.Split(hl, "\n") {
			for _, wr := range strings.Split(ansi.Hardwrap(row, width, true), "\n") {
				rows = append(rows, chatCodeLine.Render(chatCodeGutter.Render("│ ")+wr))
			}
		}
		out = append(out, strings.Join(rows, "\n"))
	}
	flushCode := func() {
		if len(code) == 0 {
			return
		}
		renderCodeRows(code, lang)
		code = nil
	}
	var indented []string
	flushIndented := func() {
		if len(indented) == 0 {
			return
		}
		renderCodeRows(indented, "")
		indented = nil
	}

	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if marker, info, ok := chatFenceOpen(trimmed); ok && (!inCode || marker == fenceMarker) {
			flushIndented()
			if inCode {
				flushCode()
				inCode = false
				fenceMarker = ""
			} else {
				flushProse()
				lang = chatFenceLang(info)
				fenceMarker = marker
				inCode = true
			}
			continue // the fence markers themselves are not shown
		}
		if inCode {
			code = append(code, ln)
			continue
		}
		// Markdown's other code idiom: 4-space-indented lines (lessons use it a
		// lot). Word-wrapping them as prose breaks identifiers mid-word, so they
		// render through the code path instead.
		if strings.HasPrefix(ln, "    ") && trimmed != "" {
			flushProse()
			indented = append(indented, strings.TrimPrefix(ln, "    "))
			continue
		}
		flushIndented()
		prose = append(prose, ln)
	}
	flushProse()
	flushIndented()
	flushCode() // tolerate an unterminated fence
	return strings.Join(out, "\n")
}

func chatFenceOpen(trimmed string) (marker, info string, ok bool) {
	switch {
	case strings.HasPrefix(trimmed, "```"):
		return "```", strings.TrimSpace(strings.TrimPrefix(trimmed, "```")), true
	case strings.HasPrefix(trimmed, "~~~"):
		return "~~~", strings.TrimSpace(strings.TrimPrefix(trimmed, "~~~")), true
	default:
		return "", "", false
	}
}

func chatFenceLang(info string) string {
	info = strings.ToLower(strings.TrimSpace(info))
	if info == "" {
		return ""
	}
	field := strings.Fields(info)[0]
	field = strings.Trim(field, "{}")
	field = strings.TrimPrefix(field, ".")
	return strings.Trim(field, " \t")
}

func (c *chatModel) focus() tea.Cmd {
	c.focused = true
	c.exitNormal() // focusing the pane means "I want to type"
	return c.input.Focus()
}

func (c *chatModel) blur() {
	c.focused = false
	c.input.Blur()
}

// submit returns the trimmed input value and clears the field. ok is false when
// the input is empty/whitespace. Submitted text joins the input history.
func (c *chatModel) submit() (text string, ok bool) {
	v := strings.TrimSpace(c.input.Value())
	c.input.Reset()
	if v == "" {
		return "", false
	}
	if n := len(c.inputHist); n == 0 || c.inputHist[n-1] != v {
		c.inputHist = append(c.inputHist, v)
	}
	c.histPos = len(c.inputHist)
	c.draft = ""
	return v, true
}

// histKey recalls previous inputs with ↑/↓, readline-style, and reports whether
// it consumed the key. To keep the arrows usable for editing multi-line input,
// history navigation engages only while the input is empty or showing an
// unmodified recalled entry.
func (c *chatModel) histKey(msg tea.KeyMsg) bool {
	key := msg.String()
	if key != "up" && key != "down" {
		return false
	}
	v := c.input.Value()
	navigable := v == "" ||
		(c.histPos < len(c.inputHist) && v == c.inputHist[c.histPos])
	if !navigable {
		return false
	}
	switch key {
	case "up":
		if c.histPos == 0 || len(c.inputHist) == 0 {
			return c.histPos < len(c.inputHist) // consume while navigating; else let the cursor move
		}
		if c.histPos == len(c.inputHist) {
			c.draft = v
		}
		c.histPos--
		c.input.SetValue(c.inputHist[c.histPos])
	case "down":
		if c.histPos >= len(c.inputHist) {
			return false
		}
		c.histPos++
		if c.histPos == len(c.inputHist) {
			c.input.SetValue(c.draft)
		} else {
			c.input.SetValue(c.inputHist[c.histPos])
		}
	}
	return true
}

// Update routes input to the transcript or the input area. Scroll keys and mouse
// wheel events drive the viewport; everything else is typing. We deliberately do
// NOT forward keystrokes to the viewport's own keymap — its defaults bind j/k/f/b
// etc., which would scroll the transcript while the learner types those letters.
func (c chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		var cmd tea.Cmd
		c.vp, cmd = c.vp.Update(msg)
		return c, cmd
	case tea.KeyMsg:
		if c.scrollKey(msg) {
			return c, nil
		}
		if c.histKey(msg) {
			return c, nil
		}
		// A light Vim layer on the input: Esc enters Normal mode (j/k scroll
		// the transcript, ":" — handled by the parent — opens the command
		// line), i/a return to typing. Insert mode is the default and exactly
		// the old behavior.
		if c.normal {
			c.normalKey(msg)
			return c, nil
		}
		if msg.Type == tea.KeyEsc {
			c.enterNormal()
			return c, nil
		}
	}

	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return c, cmd
}

// inNormal reports whether the input sits in Normal mode (the parent routes
// ":" to the command line and withholds Enter-to-send while it does).
func (c chatModel) inNormal() bool { return c.normal }

func (c *chatModel) enterNormal() {
	c.normal = true
	c.input.FocusedStyle.Prompt = chatPromptNormal // green: "you're in Normal"
}

func (c *chatModel) exitNormal() {
	c.normal = false
	c.input.FocusedStyle.Prompt = chatPromptFocus
}

// normalKey handles one Normal-mode key, Vim-style over the DRAFT: motions
// (h/l/j/k, w/b/e, 0/^/$), edits (x, D, dd, cc, C), and the Insert entries
// (i/a/I/A). The transcript keeps its mode-independent keys: Ctrl-D/U/F/B
// page it, the mouse wheel and drag scroll it.
func (c *chatModel) normalKey(msg tea.KeyMsg) {
	send := func(t tea.KeyType) { c.input, _ = c.input.Update(tea.KeyMsg{Type: t}) }
	alt := func(r rune) {
		c.input, _ = c.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true})
	}
	switch msg.String() {
	// --- enter Insert ---
	case "i":
		c.exitNormal()
	case "a":
		send(tea.KeyRight)
		c.exitNormal()
	case "I":
		send(tea.KeyHome)
		c.exitNormal()
	case "A":
		send(tea.KeyEnd)
		c.exitNormal()
	// --- motions ---
	case "h", "left":
		send(tea.KeyLeft)
	case "l", "right":
		send(tea.KeyRight)
	case "j", "down":
		send(tea.KeyDown)
	case "k", "up":
		send(tea.KeyUp)
	case "w", "e":
		alt('f') // the textarea's word-forward
	case "b":
		alt('b') // word-backward
	case "0", "^":
		send(tea.KeyHome)
	case "$":
		send(tea.KeyEnd)
	// --- edits ---
	case "x":
		send(tea.KeyDelete)
	case "D":
		send(tea.KeyCtrlK) // delete to end of line
	case "d": // dd: clear the current line (two-key chord)
		if c.pendingOp == 'd' {
			c.pendingOp = 0
			send(tea.KeyHome)
			send(tea.KeyCtrlK)
			return
		}
		c.pendingOp = 'd'
		return
	case "c": // cc: change the line
		if c.pendingOp == 'c' {
			c.pendingOp = 0
			send(tea.KeyHome)
			send(tea.KeyCtrlK)
			c.exitNormal()
			return
		}
		c.pendingOp = 'c'
		return
	case "C":
		send(tea.KeyCtrlK)
		c.exitNormal()
	// --- transcript jumps ---
	case "g":
		c.vp.GotoTop()
	case "G":
		c.vp.GotoBottom()
	}
	c.pendingOp = 0 // any other key cancels a half-typed dd/cc
}

// scrollKey handles transcript scrolling and reports whether it consumed the key.
// Bindings are vim-flavored — Ctrl+D/U for a half page, Ctrl+F/B for a full page —
// because the focused input box owns j/k, g/G, and the plain arrows. PgUp/PgDn and
// Shift+↑/↓ (single line) are accepted too for non-vim muscle memory.
func (c *chatModel) scrollKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "ctrl+d":
		c.vp.HalfViewDown()
	case "ctrl+u":
		c.vp.HalfViewUp()
	case "ctrl+f", "pgdown":
		c.vp.ViewDown()
	case "ctrl+b", "pgup":
		c.vp.ViewUp()
	case "shift+down":
		c.vp.ScrollDown(1)
	case "shift+up":
		c.vp.ScrollUp(1)
	default:
		return false
	}
	return true
}

// --- streaming replies ---

// beginStream opens an empty tutor block that appendStream grows in place as
// model output arrives.
func (c *chatModel) beginStream() {
	c.append(roleTutor, "")
}

// appendStream adds a streamed chunk to the block opened by beginStream,
// following the tail only if the reader was already at the bottom.
func (c *chatModel) appendStream(delta string) {
	if len(c.blocks) == 0 {
		return
	}
	follow := c.vp.AtBottom()
	c.blocks[len(c.blocks)-1].text += delta
	c.reflow()
	if follow {
		c.vp.GotoBottom()
	}
}

// failStream replaces the in-progress streamed block with an error notice.
func (c *chatModel) failStream(text string) {
	if len(c.blocks) == 0 {
		return
	}
	c.blocks[len(c.blocks)-1] = chatBlock{role: roleSystem, text: text}
	c.reflow()
	c.vp.GotoBottom()
}

// --- copying replies ---

// lastReply returns the most recent tutor or lesson message.
func (c chatModel) lastReply() (string, bool) {
	for i := len(c.blocks) - 1; i >= 0; i-- {
		if r := c.blocks[i].role; r == roleTutor || r == roleLesson {
			return c.blocks[i].text, true
		}
	}
	return "", false
}

// lastCode returns the last fenced code block from the most recent tutor or
// lesson message that contains one.
func (c chatModel) lastCode() (string, bool) {
	for i := len(c.blocks) - 1; i >= 0; i-- {
		if r := c.blocks[i].role; r != roleTutor && r != roleLesson {
			continue
		}
		if code, ok := lastFence(c.blocks[i].text); ok {
			return code, true
		}
	}
	return "", false
}

// lastFence extracts the contents of the LAST ``` fence in text.
func lastFence(text string) (string, bool) {
	var blocks []string
	var cur []string
	inCode := false
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			if inCode {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = nil
			}
			inCode = !inCode
			continue
		}
		if inCode {
			cur = append(cur, ln)
		}
	}
	if inCode && len(cur) > 0 { // tolerate an unterminated fence
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	if len(blocks) == 0 {
		return "", false
	}
	return blocks[len(blocks)-1], true
}

// transcript renders the whole conversation as plain labeled text.
func (c chatModel) transcript() (string, bool) {
	if len(c.blocks) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(c.blocks))
	for _, b := range c.blocks {
		label := ""
		switch b.role {
		case roleUser:
			label = "you: "
		case roleTutor:
			label = "tutor: "
		case roleLesson:
			label = "lesson: "
		case roleQuiz:
			label = "quiz: "
		}
		parts = append(parts, label+b.text)
	}
	return strings.Join(parts, "\n\n"), true
}

// copyChat copies part of the transcript to the system clipboard — what is ""
// (last tutor/lesson reply), "code" (last fenced block), or "all" (the whole
// conversation) — and returns a status notice describing the outcome.
func copyChat(c *chatModel, what string) string {
	var (
		text  string
		ok    bool
		label string
	)
	switch what {
	case "code":
		text, ok = c.lastCode()
		label = "last code block"
	case "all":
		text, ok = c.transcript()
		label = "transcript"
	default:
		text, ok = c.lastReply()
		label = "last reply"
	}
	if !ok {
		if what == "code" {
			return "no code block found in the tutor's replies"
		}
		return "nothing to copy yet — ask the tutor something first"
	}
	if err := copyToClipboard(text); err != nil {
		// The native clipboard failed (e.g. headless/SSH) but the OSC 52 escape
		// was still sent; supporting terminals will have copied it.
		return "✓ sent " + label + " to the terminal clipboard (OSC 52) — native clipboard unavailable: " + err.Error()
	}
	return "✓ copied " + label + " (" + itoa(len([]rune(text))) + " chars)"
}

// pasteChat inserts the system clipboard into the chat input (":paste"), so a
// question can be composed from text copied elsewhere. (Ctrl-V in the input
// also pastes, via the textarea's own binding.) Returns a status notice, or ""
// on a silent success.
func pasteChat(c *chatModel) string {
	text, err := pasteFromClipboard()
	if err != nil {
		return "⚠ could not read the clipboard: " + err.Error()
	}
	if strings.TrimSpace(text) == "" {
		return "clipboard is empty"
	}
	c.input.InsertString(text)
	return ""
}

func (c chatModel) view() string {
	parts := make([]string, 0, 4)
	parts = append(parts, c.overlaySelection(c.vp.View()))
	if c.busy != "" {
		parts = append(parts, c.busyLine())
	}
	parts = append(parts, c.inputRule(), c.inputView())
	return strings.Join(parts, "\n")
}

// inputView renders the typing area on a full-width grey wash. The textarea
// sprays reset codes (\e[0m) mid-line — after a placeholder, between segments,
// around its own padding — and every reset drops the background, so a plain
// outer Background()/Width() wrap leaves uncolored gaps wherever the cursor
// isn't. Instead we re-assert chatInputBGSeq after each reset and pad every
// line out to the pane width ourselves, so the wash stays solid regardless of
// cursor position or focus.
func (c chatModel) inputView() string {
	w := c.w
	if w < 1 {
		w = 1
	}
	lines := strings.Split(c.input.View(), "\n")
	for i, line := range lines {
		styled := chatInputBGSeq + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+chatInputBGSeq)
		if pad := w - ansi.StringWidth(line); pad > 0 {
			styled += strings.Repeat(" ", pad)
		}
		lines[i] = styled + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}

// inputRule is the dim horizontal separator drawn between the transcript and
// the typing area, spanning the pane width.
func (c chatModel) inputRule() string {
	w := c.w
	if w < 1 {
		w = 1
	}
	return chatInputRule.Render(strings.Repeat("─", w))
}

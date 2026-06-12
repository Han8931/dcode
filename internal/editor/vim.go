package editor

// vim.go implements the parts of Vim that need real text math rather than
// synthetic textarea keys: word motions with Vim semantics (w = start of the
// NEXT word, e = end of word, b = start of the previous word) and an internal
// unnamed register so deletes/yanks can be pasted with p/P. Motions treat any
// whitespace run (including line breaks) as a separator — i.e. big-WORD
// semantics for both w and W, a deliberate simplification.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// tabIndent is inserted when Tab is pressed in Insert (or default) mode. The
// textarea has no native Tab handling — KeyTab is not a rune key — so without
// this, Tab silently does nothing.
const tabIndent = "    "

// --- cursor position & movement over the textarea ---

// cursorPos returns the cursor's logical (row, col) in rune terms. LineInfo
// reports the visual position within a soft-wrapped row; StartColumn +
// ColumnOffset recovers the logical column.
func (m *Model) cursorPos() (row, col int) {
	li := m.ta.LineInfo()
	return m.ta.Line(), li.StartColumn + li.ColumnOffset
}

// moveTo places the cursor at logical (row, col). Vertical movement goes
// through CursorUp/Down (which step visual lines), so a guard bounds the loop
// on soft-wrapped rows; SetCursor clamps the column to the line length.
// repositionMsg is a no-op message pumped through the textarea's Update: the
// textarea repositions its viewport only there, so direct CursorUp/CursorDown
// moves (moveTo) would otherwise leave a long jump ({, }, w, search) with the
// cursor stranded outside the visible window.
type repositionMsg struct{}

func (m *Model) moveTo(row, col int) {
	for guard := 0; m.ta.Line() < row && guard < 100000; guard++ {
		before := m.ta.Line()
		m.ta.CursorDown()
		if m.ta.Line() == before && m.ta.LineInfo().RowOffset == 0 {
			break // cannot advance further
		}
	}
	for guard := 0; m.ta.Line() > row && guard < 100000; guard++ {
		before := m.ta.Line()
		m.ta.CursorUp()
		if m.ta.Line() == before && m.ta.LineInfo().RowOffset == 0 {
			break
		}
	}
	m.ta.SetCursor(col)
	m.ta, _ = m.ta.Update(repositionMsg{}) // scroll the viewport to the cursor
}

// --- Vim word motions (pure text math, unit-testable) ---

// flatIndex converts (row, col) to an index into text's rune slice, where rows
// are separated by '\n'. col is clamped to the row length.
func flatIndex(runes []rune, row, col int) int {
	lineStart, r := 0, 0
	for i := 0; i < len(runes) && r < row; i++ {
		if runes[i] == '\n' {
			r++
			lineStart = i + 1
		}
	}
	lineLen := 0
	for i := lineStart; i < len(runes) && runes[i] != '\n'; i++ {
		lineLen++
	}
	if col > lineLen {
		col = lineLen
	}
	return lineStart + col
}

// rowColOf converts a rune index back to (row, col).
func rowColOf(runes []rune, idx int) (row, col int) {
	if idx > len(runes) {
		idx = len(runes)
	}
	for i := 0; i < idx; i++ {
		if runes[i] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	return row, col
}

func isWordSpace(r rune) bool { return unicode.IsSpace(r) }

// nextWordStart returns the index of the start of the next word after idx
// (Vim's w): skip the rest of the current word, then any whitespace.
func nextWordStart(runes []rune, idx int) int {
	n := len(runes)
	if n == 0 {
		return 0
	}
	i := idx
	for i < n && !isWordSpace(runes[i]) {
		i++
	}
	for i < n && isWordSpace(runes[i]) {
		i++
	}
	if i >= n {
		i = n - 1
	}
	return i
}

// nextWordEnd returns the index of the last rune of the next word end after
// idx (Vim's e): step off the current position, skip whitespace, then run to
// the final rune of that word.
func nextWordEnd(runes []rune, idx int) int {
	n := len(runes)
	if n == 0 {
		return 0
	}
	i := idx + 1
	for i < n && isWordSpace(runes[i]) {
		i++
	}
	if i >= n {
		return n - 1
	}
	for i+1 < n && !isWordSpace(runes[i+1]) {
		i++
	}
	return i
}

// prevWordStart returns the index of the start of the previous word before idx
// (Vim's b): step back, skip whitespace, then run to the word's first rune.
func prevWordStart(runes []rune, idx int) int {
	i := idx - 1
	for i > 0 && isWordSpace(runes[i]) {
		i--
	}
	for i > 0 && !isWordSpace(runes[i-1]) {
		i--
	}
	if i < 0 {
		i = 0
	}
	return i
}

// motion applies one of the pure word motions to the live cursor.
func (m *Model) motion(move func([]rune, int) int) {
	runes := []rune(m.ta.Value())
	row, col := m.cursorPos()
	target := move(runes, flatIndex(runes, row, col))
	r, c := rowColOf(runes, target)
	m.moveTo(r, c)
}

// applyMotion handles a cursor-movement key shared by Normal and Visual mode.
// It reports whether the key was a motion (and was applied).
func (m *Model) applyMotion(key string) bool {
	switch key {
	case "h", "left":
		m.arrow(tea.KeyLeft)
	case "l", "right":
		m.arrow(tea.KeyRight)
	case "j", "down":
		m.arrow(tea.KeyDown)
	case "k", "up":
		m.arrow(tea.KeyUp)
	case "w", "W":
		m.motion(nextWordStart)
	case "e", "E":
		m.motion(nextWordEnd)
	case "b", "B":
		m.motion(prevWordStart)
	case "0", "^", "_":
		m.ta.CursorStart()
	case "$":
		m.ta.CursorEnd()
		m.clampCharwise() // Vim's $ sits ON the last char, not past it
	case "G":
		m.send(tea.KeyCtrlEnd) // jump to end of buffer
	case "{":
		m.paragraph(-1)
	case "}":
		m.paragraph(1)
	case "ctrl+e":
		// One-line scrolls. The textarea's viewport follows the cursor and has
		// no public scroll API, so these move the cursor line by line (the
		// view scrolls with it at the window edges) — Vim's Ctrl-E/Ctrl-Y
		// under scrolloff, near enough.
		m.arrow(tea.KeyDown)
	case "ctrl+y":
		m.arrow(tea.KeyUp)
	default:
		return false
	}
	return true
}

// paragraph implements Vim's { and } motions: jump to the previous/next blank
// line, skipping any blank run the cursor is already in, clamped to the buffer
// edges. Being a motion, it composes with counts (3}) and Visual selections.
func (m *Model) paragraph(dir int) {
	lines := strings.Split(m.ta.Value(), "\n")
	blank := func(i int) bool { return strings.TrimSpace(lines[i]) == "" }
	row, _ := m.cursorPos()
	i := row + dir
	for i >= 0 && i < len(lines) && blank(i) {
		i += dir // step out of the current blank run
	}
	for i >= 0 && i < len(lines) && !blank(i) {
		i += dir // cross the paragraph to the blank line beyond it
	}
	if i < 0 {
		i = 0
	}
	if i >= len(lines) {
		i = len(lines) - 1
	}
	m.moveTo(i, 0)
}

// centerView places the cursor at (row, col) with its line roughly mid-
// screen. SetValue resets the textarea's viewport to the top, and a plain
// moveTo would re-scroll only until the cursor reaches the view's bottom edge
// — leaving the window somewhere the learner wasn't (the post-undo "screen
// jumped" effect). The viewport has no public scroll API, but it always
// anchors to the cursor: jumping first to a row half a window below pins that
// row to the bottom edge, so stepping back up leaves the target centered.
func (m *Model) centerView(row, col int) {
	below := row + m.ta.Height()/2
	if last := strings.Count(m.ta.Value(), "\n"); below > last {
		below = last
	}
	m.moveTo(below, 0)
	m.moveTo(row, col)
}

// --- jumplist (Ctrl-O / Ctrl-I) ---

// jumpPos is one jumplist entry: a buffer position.
type jumpPos struct{ row, col int }

func (m *Model) curJumpPos() jumpPos {
	row, col := m.cursorPos()
	return jumpPos{row, col}
}

// recordJump pushes the origin of a jump command onto the jumplist. Making a
// new jump while walked back in history truncates the forward entries, like
// an undo stack; consecutive duplicates collapse.
func (m *Model) recordJump(from jumpPos) {
	m.jumps = m.jumps[:m.jumpIdx]
	if n := len(m.jumps); n == 0 || m.jumps[n-1] != from {
		m.jumps = append(m.jumps, from)
	}
	if len(m.jumps) > 100 {
		m.jumps = m.jumps[1:]
	}
	m.jumpIdx = len(m.jumps)
}

// jumpBack moves to the previous jumplist position (Ctrl-O), stashing the
// live position first so Ctrl-I can return to it.
func (m *Model) jumpBack() {
	if m.jumpIdx == 0 {
		m.status = "at oldest jump"
		return
	}
	if m.jumpIdx == len(m.jumps) {
		m.jumps = append(m.jumps, m.curJumpPos())
	}
	m.jumpIdx--
	p := m.jumps[m.jumpIdx]
	m.moveTo(p.row, p.col)
}

// jumpForward moves to the next jumplist position (Ctrl-I / Tab).
func (m *Model) jumpForward() {
	if m.jumpIdx+1 >= len(m.jumps) {
		m.status = "at newest jump"
		return
	}
	m.jumpIdx++
	p := m.jumps[m.jumpIdx]
	m.moveTo(p.row, p.col)
}

// --- charwise cursor discipline ---

// clampCharwise pulls a cursor sitting past the line's last character back onto
// it. The textarea's cursor is an insert position (0..len); Vim's Normal-mode
// cursor sits ON a character (0..len-1). Charwise operations (x, r, ~) and the
// $ motion need the Vim view, or they act on the newline instead of the last
// character.
func (m *Model) clampCharwise() {
	lines := strings.Split(m.ta.Value(), "\n")
	row, col := m.cursorPos()
	if row < 0 || row >= len(lines) {
		return
	}
	if n := len([]rune(lines[row])); n > 0 && col >= n {
		m.ta.SetCursor(n - 1)
	}
}

// deleteChars implements x: cut n characters from the cursor into the register,
// bounded by the end of the line (Vim's x never crosses a newline).
func (m *Model) deleteChars(n int) {
	m.clampCharwise()
	lines := strings.Split(m.ta.Value(), "\n")
	row, col := m.cursorPos()
	if row < 0 || row >= len(lines) {
		return
	}
	line := []rune(lines[row])
	if len(line) == 0 || col >= len(line) {
		return // empty line: x is a no-op, as in Vim
	}
	end := col + n
	if end > len(line) {
		end = len(line)
	}
	m.pushUndo()
	m.register = string(line[col:end])
	m.regLinewise = false
	lines[row] = string(line[:col]) + string(line[end:])
	m.ta.SetValue(strings.Join(lines, "\n"))
	// Cursor stays put, clamped onto the (new) last character if needed.
	c := col
	if l := len([]rune(lines[row])); c >= l {
		c = l - 1
		if c < 0 {
			c = 0
		}
	}
	m.moveTo(row, c)
}

// --- counts, char-find, join, case, search ---

// takeCount consumes the pending numeric prefix (e.g. the 3 in 3w), defaulting
// to 1.
func (m *Model) takeCount() int {
	n := m.count
	m.count = 0
	if n < 1 {
		return 1
	}
	return n
}

// findChar implements f/F/t/T on the current line: move to (or till before) the
// n-th occurrence of ch forward (f/t) or backward (F/T) from the cursor.
func (m *Model) findChar(op rune, ch rune, n int) bool {
	lines := strings.Split(m.ta.Value(), "\n")
	row, col := m.cursorPos()
	if row < 0 || row >= len(lines) {
		return false
	}
	line := []rune(lines[row])

	step, till := 1, false
	switch op {
	case 'F':
		step = -1
	case 't':
		till = true
	case 'T':
		step, till = -1, true
	}

	pos := col
	for found := 0; found < n; {
		pos += step
		if pos < 0 || pos >= len(line) {
			return false
		}
		if line[pos] == ch {
			found++
		}
	}
	if till {
		pos -= step // stop one short of the target, Vim's t/T
	}
	if pos < 0 || pos >= len(line) {
		return false
	}
	m.moveTo(row, pos)
	m.lastFindOp, m.lastFindCh = op, ch
	return true
}

// repeatFind implements ; (same direction) and , (reversed).
func (m *Model) repeatFind(reverse bool, n int) {
	if m.lastFindOp == 0 {
		return
	}
	op := m.lastFindOp
	if reverse {
		switch op {
		case 'f':
			op = 'F'
		case 'F':
			op = 'f'
		case 't':
			op = 'T'
		case 'T':
			op = 't'
		}
	}
	saveOp, saveCh := m.lastFindOp, m.lastFindCh
	m.findChar(op, m.lastFindCh, n)
	m.lastFindOp, m.lastFindCh = saveOp, saveCh // , must not flip the stored direction
}

// joinLines implements J: join the current line with the next, separated by a
// single space (n times).
func (m *Model) joinLines(n int) {
	for ; n > 0; n-- {
		lines := strings.Split(m.ta.Value(), "\n")
		row, _ := m.cursorPos()
		if row < 0 || row+1 >= len(lines) {
			return
		}
		left := strings.TrimRight(lines[row], " \t")
		right := strings.TrimLeft(lines[row+1], " \t")
		joined := left + " " + right
		if left == "" {
			joined = right
		}
		out := append(append(append([]string{}, lines[:row]...), joined), lines[row+2:]...)
		m.ta.SetValue(strings.Join(out, "\n"))
		m.moveTo(row, len([]rune(left))) // cursor at the join point, as in Vim
	}
}

// toggleCase implements ~: flip the case of the rune under the cursor and
// advance, n times.
func (m *Model) toggleCase(n int) {
	m.clampCharwise()
	for ; n > 0; n-- {
		lines := strings.Split(m.ta.Value(), "\n")
		row, col := m.cursorPos()
		if row < 0 || row >= len(lines) {
			return
		}
		line := []rune(lines[row])
		if col < 0 || col >= len(line) {
			return
		}
		r := line[col]
		switch {
		case unicode.IsLower(r):
			line[col] = unicode.ToUpper(r)
		case unicode.IsUpper(r):
			line[col] = unicode.ToLower(r)
		}
		lines[row] = string(line)
		m.ta.SetValue(strings.Join(lines, "\n"))
		m.moveTo(row, col+1)
	}
}

// search jumps to the next occurrence of query (wrapping), in direction dir
// (+1 forward for / and n, -1 backward for N). Returns false when not found.
func (m *Model) search(query string, dir int) bool {
	if query == "" {
		return false
	}
	text := []rune(m.ta.Value())
	q := []rune(query)
	row, col := m.cursorPos()
	start := flatIndex(text, row, col)

	at := func(i int) bool {
		if i < 0 || i+len(q) > len(text) {
			return false
		}
		return string(text[i:i+len(q)]) == query
	}
	n := len(text)
	for step := 1; step <= n; step++ {
		i := start + dir*step
		// wrap around the buffer
		i = ((i % n) + n) % n
		if at(i) {
			r, c := rowColOf(text, i)
			m.moveTo(r, c)
			return true
		}
	}
	return false
}

// --- undo / redo ---

// editState is one undo snapshot: the whole buffer plus the cursor.
type editState struct {
	value    string
	row, col int
}

const maxUndo = 100

// pushUndo snapshots the current buffer before a mutating operation. Insert
// mode pushes once on entry, so a whole typing session undoes as one unit
// (like Vim). Starting a new change invalidates the redo stack.
func (m *Model) pushUndo() {
	row, col := m.cursorPos()
	st := editState{value: m.ta.Value(), row: row, col: col}
	if n := len(m.undoStack); n > 0 && m.undoStack[n-1].value == st.value {
		m.undoStack[n-1] = st // same text: just refresh the cursor
	} else {
		m.undoStack = append(m.undoStack, st)
		if len(m.undoStack) > maxUndo {
			m.undoStack = m.undoStack[1:]
		}
	}
	m.redoStack = nil
}

// undo restores the most recent snapshot that differs from the buffer.
func (m *Model) undo() {
	cur := m.ta.Value()
	for len(m.undoStack) > 0 {
		st := m.undoStack[len(m.undoStack)-1]
		m.undoStack = m.undoStack[:len(m.undoStack)-1]
		if st.value == cur {
			continue // no-op snapshot (e.g. an operation that changed nothing)
		}
		row, col := m.cursorPos()
		m.redoStack = append(m.redoStack, editState{value: cur, row: row, col: col})
		m.ta.SetValue(st.value)
		m.centerView(st.row, st.col)
		return
	}
	m.status = "already at oldest change"
}

// redo re-applies the change most recently undone.
func (m *Model) redo() {
	cur := m.ta.Value()
	for len(m.redoStack) > 0 {
		st := m.redoStack[len(m.redoStack)-1]
		m.redoStack = m.redoStack[:len(m.redoStack)-1]
		if st.value == cur {
			continue
		}
		row, col := m.cursorPos()
		m.undoStack = append(m.undoStack, editState{value: cur, row: row, col: col})
		m.ta.SetValue(st.value)
		m.centerView(st.row, st.col)
		return
	}
	m.status = "already at newest change"
}

// clearHistory drops the undo/redo stacks — called when the buffer is replaced
// wholesale (switching notes/challenges), so undo never crosses files.
func (m *Model) clearHistory() {
	m.undoStack, m.redoStack = nil, nil
}

// --- auto-indent for o/O ---

// lineIndent returns the leading whitespace of the cursor's current line, so
// o/O can open the new line at the same indentation.
func (m *Model) lineIndent() string {
	lines := strings.Split(m.ta.Value(), "\n")
	r := m.ta.Line()
	if r < 0 || r >= len(lines) {
		return ""
	}
	return leadingWS(lines[r])
}

func leadingWS(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

// autoIndentFor returns the indentation a NEW line should get when opened after
// the given text: the text's own leading whitespace, one level deeper when it
// ends with an indent-opening token — '{', '(' and '[' for brace languages,
// ':' for Python blocks (and Go labels/cases).
func autoIndentFor(text string) string {
	indent := leadingWS(text)
	t := strings.TrimRight(text, " \t")
	if t == "" {
		return indent
	}
	switch t[len(t)-1] {
	case '{', '(', '[', ':':
		indent += tabIndent
	}
	return indent
}

// insertNewlineIndented handles Enter in Insert mode: split the line and indent
// the new one automatically (Vim's autoindent + an extra level after openers).
func (m *Model) insertNewlineIndented() {
	lines := strings.Split(m.ta.Value(), "\n")
	row, col := m.cursorPos()
	prefix := ""
	if row >= 0 && row < len(lines) {
		line := []rune(lines[row])
		if col > len(line) {
			col = len(line)
		}
		prefix = string(line[:col])
	}
	m.send(tea.KeyEnter)
	if indent := autoIndentFor(prefix); indent != "" {
		m.ta.InsertString(indent)
	}
}

// electricClose dedents the current line one level when the learner types a
// closing brace on a line that is so far only indentation — so "}" lands at
// the block's opening depth, as in Vim/IDEs. The brace itself is inserted by
// the caller afterwards.
func (m *Model) electricClose() {
	lines := strings.Split(m.ta.Value(), "\n")
	row, col := m.cursorPos()
	if row < 0 || row >= len(lines) {
		return
	}
	line := []rune(lines[row])
	if col > len(line) {
		col = len(line)
	}
	prefix := string(line[:col])
	if prefix == "" || strings.TrimSpace(prefix) != "" {
		return // not at the start-of-line indentation
	}
	dedented := shiftLine(prefix, -1)
	if dedented == prefix {
		return
	}
	lines[row] = dedented + string(line[col:])
	m.ta.SetValue(strings.Join(lines, "\n"))
	m.moveTo(row, len([]rune(dedented)))
}

// --- Visual mode ---

// enterVisual starts a selection anchored at the cursor. linewise selects whole
// lines (V); otherwise the selection is charwise (v).
func (m *Model) enterVisual(linewise bool) {
	m.anchorRow, m.anchorCol = m.cursorPos()
	m.visualLine = linewise
	m.mode = modeVisual
	m.syncVisualTop() // open on the textarea's current window, not a recomputed one
	m.scrollVisual()
}

// updateVisual routes a key while a selection is active: motions extend it,
// operators act on it, Esc abandons it.
func (m Model) updateVisual(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A pending prefix ('g' for gg, ',' for the leader chords) is completed or
	// cancelled by the next key.
	if m.pending != 0 {
		op := m.pending
		m.pending = 0
		if op == 'g' && msg.String() == "g" {
			m.send(tea.KeyCtrlHome)
			m.scrollVisual()
		}
		// ",d" decodes the selection: capture it and forward a :decode command,
		// the same path ":decode" from the command line takes.
		if op == ',' && msg.String() == "d" {
			m.captureSelection()
			sel := m.selCapture
			m.mode = modeNormal
			return m, func() tea.Msg {
				rc := RunCommandMsg{Raw: "decode"}
				if sel != nil {
					rc.Sel = &Selection{Text: sel.text, Start: sel.start, Cut: sel.cut}
				}
				return rc
			}
		}
		return m, nil
	}

	if msg.Type == tea.KeyEsc {
		m.mode = modeNormal
		return m, nil
	}
	if m.applyMotion(msg.String()) {
		m.scrollVisual() // keep the Visual window following the cursor
		return m, nil
	}

	switch msg.String() {
	case ":":
		// Open the command line on the selection, Vim's ":'<,'>" — capture the
		// span so a forwarded command (e.g. ":edit …") can act on it.
		m.captureSelection()
		m.mode = modeCommand
		m.cmd.SetValue("")
		m.cmd.Focus()
		m.exHist.Open()
		return m, textinput.Blink
	case "v":
		if m.visualLine {
			m.visualLine = false // switch V -> v, keeping the selection
		} else {
			m.mode = modeNormal
		}
	case "V":
		if m.visualLine {
			m.mode = modeNormal
		} else {
			m.visualLine = true // switch v -> V
		}
	case "o":
		// Swap the cursor and the anchor, Vim-style, to grow the other end.
		row, col := m.cursorPos()
		ar, ac := m.anchorRow, m.anchorCol
		m.anchorRow, m.anchorCol = row, col
		m.moveTo(ar, ac)
	case "d", "x":
		m.pushUndo()
		m.visualDelete()
		m.mode = modeNormal
	case "y":
		m.visualYank()
		m.mode = modeNormal
	case "c":
		m.pushUndo()
		linewise := m.visualLine
		m.visualDelete()
		if linewise {
			// cc-style change: keep an empty line to type into.
			m.ta.CursorStart()
			m.ta.InsertString("\n")
			m.arrow(tea.KeyUp)
		}
		m.mode = modeInsert
	case "<":
		m.pushUndo()
		m.visualIndent(-1)
		m.mode = modeNormal
	case ">":
		m.pushUndo()
		m.visualIndent(1)
		m.mode = modeNormal
	case "g":
		m.pending = 'g'
	case ",":
		m.pending = ',' // leader: ",d" decodes the selection
	}
	return m, nil
}

// visualSpan returns the buffer runes and the selection as a half-open range
// [start, cut). Charwise selections include the rune under both ends; linewise
// selections cover whole lines including one bounding newline so deleting the
// span removes the lines entirely.
func (m *Model) visualSpan() (runes []rune, start, cut int) {
	runes = []rune(m.ta.Value())
	row, col := m.cursorPos()
	a := flatIndex(runes, m.anchorRow, m.anchorCol)
	b := flatIndex(runes, row, col)
	if a > b {
		a, b = b, a
	}
	if !m.visualLine {
		cut = b + 1
		if cut > len(runes) {
			cut = len(runes)
		}
		return runes, a, cut
	}
	start = a
	for start > 0 && runes[start-1] != '\n' {
		start--
	}
	cut = b
	for cut < len(runes) && runes[cut] != '\n' {
		cut++
	}
	switch {
	case cut < len(runes):
		cut++ // include the trailing newline
	case start > 0:
		start-- // last line: take the preceding newline instead
	}
	return runes, start, cut
}

// captureSelection stashes the current Visual selection — its exact text and
// flat-rune range, with any bounding newlines (from a linewise V selection)
// trimmed off both — for a command launched with ":" from Visual mode. An
// empty selection captures nothing.
func (m *Model) captureSelection() {
	runes, start, cut := m.visualSpan()
	for start < cut && runes[start] == '\n' {
		start++
	}
	for cut > start && runes[cut-1] == '\n' {
		cut--
	}
	if start >= cut {
		m.selCapture = nil
		return
	}
	m.selCapture = &selSpan{text: string(runes[start:cut]), start: start, cut: cut}
}

// ReplaceRange swaps the half-open flat-rune span [start, cut) for repl as one
// undoable edit — but ONLY if that span still holds want, so an edit proposed
// against an earlier buffer can't clobber text that changed underneath it. It
// reports whether the replacement happened; on a mismatch the buffer is left
// untouched. The cursor lands at the end of the inserted text.
func (m *Model) ReplaceRange(start, cut int, repl, want string) bool {
	runes := []rune(m.ta.Value())
	if start < 0 || start > cut || cut > len(runes) || string(runes[start:cut]) != want {
		return false
	}
	m.pushUndo()
	out := append(append(append([]rune{}, runes[:start]...), []rune(repl)...), runes[cut:]...)
	m.ta.SetValue(string(out))
	r, c := rowColOf(out, start+len([]rune(repl)))
	m.moveTo(r, c)
	return true
}

// visualDelete removes the selection into the register and leaves the cursor at
// the selection start.
func (m *Model) visualDelete() {
	runes, start, cut := m.visualSpan()
	if start >= cut {
		return
	}
	text := string(runes[start:cut])
	if m.visualLine {
		text = strings.Trim(text, "\n")
	}
	m.register, m.regLinewise = text, m.visualLine

	rest := append(append([]rune{}, runes[:start]...), runes[cut:]...)
	m.ta.SetValue(string(rest))
	idx := start
	if idx > len(rest) {
		idx = len(rest)
	}
	r, c := rowColOf(rest, idx)
	m.moveTo(r, c)
}

// visualYank copies the selection into the register (buffer unchanged) and, as
// in Vim, moves the cursor to the selection start.
func (m *Model) visualYank() {
	runes, start, cut := m.visualSpan()
	if start >= cut {
		return
	}
	text := string(runes[start:cut])
	if m.visualLine {
		text = strings.Trim(text, "\n")
	}
	m.setYank(text, m.visualLine)
	r, c := rowColOf(runes, start)
	m.moveTo(r, c)
}

// visualIndent shifts every line the selection touches by one level and leaves
// the cursor on the first of them.
func (m *Model) visualIndent(delta int) {
	row, _ := m.cursorPos()
	r1, r2 := m.anchorRow, row
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	lines := strings.Split(m.ta.Value(), "\n")
	for i := r1; i <= r2 && i < len(lines); i++ {
		lines[i] = shiftLine(lines[i], delta)
	}
	m.ta.SetValue(strings.Join(lines, "\n"))
	m.moveTo(r1, 0)
}

// shiftLine indents (+1) or dedents (-1) a single line by one level.
func shiftLine(line string, delta int) string {
	if delta > 0 {
		if line == "" {
			return line // Vim leaves empty lines alone when indenting
		}
		return tabIndent + line
	}
	if strings.HasPrefix(line, "\t") {
		return line[1:]
	}
	for i := 0; i < len(tabIndent); i++ {
		if !strings.HasPrefix(line, " ") {
			break
		}
		line = line[1:]
	}
	return line
}

// --- line indentation (Vim's << and >>) ---

// indentLines shifts n lines starting at the cursor one indent level: +1
// prepends tabIndent (>>), -1 removes up to one level of leading whitespace —
// a tab or up to len(tabIndent) spaces (<<). The cursor stays on the same
// character of the current line.
func (m *Model) indentLines(n, delta int) {
	row, col := m.cursorPos()
	lines := strings.Split(m.ta.Value(), "\n")
	if row < 0 || row >= len(lines) {
		return
	}
	changed := false
	firstDelta := 0
	for i := 0; i < n && row+i < len(lines); i++ {
		old := lines[row+i]
		lines[row+i] = shiftLine(old, delta)
		if lines[row+i] != old {
			changed = true
			if i == 0 {
				firstDelta = len(lines[row+i]) - len(old) // indent is ASCII: bytes == runes
			}
		}
	}
	if !changed {
		return
	}
	if col += firstDelta; col < 0 {
		col = 0
	}
	m.ta.SetValue(strings.Join(lines, "\n"))
	m.moveTo(row, col)
}

// --- Visual-mode rendering ---

// Visual-mode styles: the selection bar, the cursor block, and the gutter.
var (
	visualSelStyle    = lipgloss.NewStyle().Background(lipgloss.Color("24")).Foreground(lipgloss.Color("231"))
	visualCursorStyle = lipgloss.NewStyle().Reverse(true)
	visualGutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	visualBadge       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("75")).Padding(0, 1)
)

// visualView renders the buffer with the active selection highlighted. The
// textarea cannot draw selections, so Visual mode takes over rendering: line
// numbers + width-aware soft wrap + a scrolled window that keeps the cursor
// visible. Editing is impossible while selecting, so the simpler renderer can't
// drift from the real buffer.
// vseg is one wrapped visual row of the Visual-mode renderer: a rune range of
// a logical line.
type vseg struct {
	lineIdx    int // logical line
	start, end int // rune range within the line
	first      bool
}

// visualLayout wraps every logical line to the Visual view's content width,
// returning the segments and the index of the one holding the cursor.
func (m Model) visualLayout() (segs []vseg, cursorSeg int) {
	lines := strings.Split(m.ta.Value(), "\n")
	curRow, curCol := m.cursorPos()
	// Wrap at the textarea's own content width with its own algorithm so the
	// Visual rows match its viewport exactly (see wrapWords).
	contentW := m.ta.Width()
	if contentW < 1 {
		contentW = 1
	}
	for li, line := range lines {
		parts := wrapWords([]rune(line), contentW)
		for si, p := range parts {
			s := vseg{lineIdx: li, start: p[0], end: p[1], first: si == 0}
			if li == curRow && curCol >= p[0] && (curCol < p[1] || si == len(parts)-1) {
				cursorSeg = len(segs)
			}
			segs = append(segs, s)
		}
	}
	return segs, cursorSeg
}

// visualGutterWidth matches the textarea's line-number gutter so entering
// Visual mode doesn't shift the text horizontally. The textarea sizes its
// gutter from MaxHeight (formatLineNumber: " %*v " with len(MaxHeight) digits),
// not the live line count, so this must mirror that — using the line count here
// produced a narrower gutter for short buffers and nudged every line left.
func (m Model) visualGutterWidth() int {
	maxHeight := m.ta.MaxHeight
	if maxHeight <= 0 {
		maxHeight = 1
	}
	return len(strconv.Itoa(maxHeight)) + 2
}

// visualHeight is the Visual window height (the textarea's row count).
func (m Model) visualHeight() int {
	if h := m.ta.Height(); h > 0 {
		return h
	}
	return 10
}

// syncVisualTop anchors the Visual window to the textarea's current viewport
// by reading the line-number gutter off its render, so entering Visual mode
// doesn't jump the view. Without a parsable gutter the cursor clamp in
// scrollVisual decides alone.
func (m *Model) syncVisualTop() {
	for k, row := range strings.Split(m.ta.View(), "\n") {
		n, ok := rowLineNumber(row, m.ta.ShowLineNumbers)
		if !ok {
			continue // a soft-wrap continuation row; the next row is numbered
		}
		segs, _ := m.visualLayout()
		for i, s := range segs {
			if s.lineIdx == n-1 && s.first {
				m.visualTop = i - k
				return
			}
		}
		return
	}
}

// scrollVisual keeps the cursor segment inside the Visual window, scrolling
// minimally like the textarea's own viewport does.
func (m *Model) scrollVisual() {
	segs, cursorSeg := m.visualLayout()
	h := m.visualHeight()
	if maxTop := len(segs) - h; m.visualTop > maxTop {
		m.visualTop = maxTop
	}
	if m.visualTop < 0 {
		m.visualTop = 0
	}
	if cursorSeg < m.visualTop {
		m.visualTop = cursorSeg
	} else if cursorSeg >= m.visualTop+h {
		m.visualTop = cursorSeg - h + 1
	}
}

// highlightVisualSeg applies the language's highlighting to one Visual-mode
// segment AFTER the selection/cursor styling: the highlighter is escape-aware
// and re-asserts the selection background after each styled token — the same
// ambient-SGR mechanism that protects the cursor-line background in normal
// rendering. states carries the markdown block context per logical line.
func (m Model) highlightVisualSeg(content string, states []lineState, lineIdx int) string {
	switch strings.ToLower(m.lang) {
	case "markdown", "md":
		st := mdState{cur: states[lineIdx]}
		return highlightMarkdownRow(content, &st)
	case "go", "golang", "python", "py":
		rules, _ := fenceRules(strings.ToLower(m.lang))
		inBlock := false
		return highlightLine(content, rules, &inBlock)
	}
	return content
}

func (m Model) visualView() string {
	lines := strings.Split(m.ta.Value(), "\n")
	curRow, curCol := m.cursorPos()
	_, selStart, selCut := m.visualSpan()

	height := m.visualHeight()
	gutterW := m.visualGutterWidth()
	segs, cursorSeg := m.visualLayout()

	// The window was anchored on entry and scrolled by updateVisual; clamp
	// defensively (m is a copy here, so this can't drift the stored state).
	top := m.visualTop
	if maxTop := len(segs) - height; top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	if cursorSeg < top {
		top = cursorSeg
	} else if cursorSeg >= top+height {
		top = cursorSeg - height + 1
	}
	bottom := top + height
	if bottom > len(segs) {
		bottom = len(segs)
	}

	var states []lineState
	if l := strings.ToLower(m.lang); l == "markdown" || l == "md" {
		states = mdScanBuffer(m.ta.Value())
	}

	flatStarts := flatLineStarts(lines)
	var b strings.Builder
	for i := top; i < bottom; i++ {
		s := segs[i]
		if i > top {
			b.WriteString("\n")
		}
		gutter := strings.Repeat(" ", gutterW)
		if s.first {
			gutter = fmt.Sprintf(" %*d ", gutterW-2, s.lineIdx+1)
		}
		b.WriteString(visualGutterStyle.Render(gutter))
		content := renderSelSegment(
			[]rune(lines[s.lineIdx]), s.start, s.end, flatStarts[s.lineIdx],
			selStart, selCut, s.lineIdx == curRow, curCol,
		)
		b.WriteString(m.highlightVisualSeg(content, states, s.lineIdx))
	}
	// Pad to the textarea's height so the surrounding layout doesn't shift.
	for i := bottom - top; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

// renderSelSegment renders one wrapped segment ([segStart, segEnd) of line),
// batching runs of equally-styled runes (normal / selected / cursor).
func renderSelSegment(line []rune, segStart, segEnd, flatStart, selStart, selCut int, cursorLine bool, curCol int) string {
	classOf := func(col int) int {
		if cursorLine && col == curCol {
			return 2 // cursor
		}
		if idx := flatStart + col; idx >= selStart && idx < selCut {
			return 1 // selected
		}
		return 0
	}
	styleOf := func(class int) *lipgloss.Style {
		switch class {
		case 1:
			return &visualSelStyle
		case 2:
			return &visualCursorStyle
		}
		return nil
	}

	var b strings.Builder
	runStart, runClass := segStart, -1
	flush := func(end int) {
		if runClass < 0 || end <= runStart {
			return
		}
		text := string(line[runStart:end])
		if st := styleOf(runClass); st != nil {
			text = st.Render(text)
		}
		b.WriteString(text)
	}
	for col := segStart; col < segEnd && col < len(line); col++ {
		c := classOf(col)
		if c != runClass {
			flush(col)
			runStart, runClass = col, c
		}
	}
	end := segEnd
	if end > len(line) {
		end = len(line)
	}
	flush(end)

	// The cursor (or a linewise selection) on the line-end position is past the
	// last rune; show it as a styled space so it stays visible.
	switch {
	case cursorLine && curCol >= len(line) && segEnd >= len(line):
		b.WriteString(visualCursorStyle.Render(" "))
	case len(line) == 0 && flatStart >= selStart && flatStart < selCut:
		b.WriteString(visualSelStyle.Render(" "))
	}
	return b.String()
}

// wrapWords splits a line of runes into [start, end) segments that match the
// bubbles textarea's word-wrap (its unexported wrap), so the Visual renderer's
// rows line up with the textarea's viewport. A plain width wrap broke lines at
// different points than the textarea — words break on spaces, not mid-word — so
// once any line wrapped, the two viewports diverged and the Visual view drifted
// a row off. This mirrors every quirk that matters for row counts: trailing
// spaces stay on the wrapped row, an over-long word is hard-broken, and a row
// that exactly fills the width is followed by an empty segment. A line always
// yields at least one (possibly empty) segment so empty lines still occupy a row.
func wrapWords(line []rune, width int) [][2]int {
	if len(line) == 0 {
		return [][2]int{{0, 0}}
	}
	if width < 1 {
		width = 1
	}
	var (
		segs   [][2]int
		start  int // original index where the open row begins
		rowLen int // original runes committed to the open row
		rowW   int // display width of the open row
		word   []rune
		wordW  int
		spaces int
	)
	commit := func(n, w int) { rowLen += n; rowW += w }
	newRow := func() {
		segs = append(segs, [2]int{start, start + rowLen})
		start += rowLen
		rowLen, rowW = 0, 0
	}
	for _, r := range line {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
			wordW += runewidth.RuneWidth(r)
		}
		switch {
		case spaces > 0:
			// A word is complete; place it (with its trailing spaces) on the
			// open row, or wrap to a new one if it no longer fits.
			if rowW+wordW+spaces > width {
				newRow()
			}
			commit(len(word)+spaces, wordW+spaces)
			word, wordW, spaces = word[:0], 0, 0
		case wordW+runewidth.RuneWidth(word[len(word)-1]) > width:
			// A single word wider than the row: hard-break it like the textarea.
			if rowLen > 0 {
				newRow()
			}
			commit(len(word), wordW)
			word, wordW = word[:0], 0
		}
	}
	// Flush the final pending word; the textarea injects a trailing space here
	// (not an original rune, so it never shifts a range) and pushes the word to a
	// fresh row when the open one is already full.
	if rowW+wordW+spaces >= width {
		newRow()
	}
	commit(len(word)+spaces, wordW+spaces)
	segs = append(segs, [2]int{start, start + rowLen})
	return segs
}

// flatLineStarts returns each line's starting index in the flat rune view of
// the buffer (lines joined by '\n').
func flatLineStarts(lines []string) []int {
	starts := make([]int, len(lines))
	idx := 0
	for i, l := range lines {
		starts[i] = idx
		idx += len([]rune(l)) + 1
	}
	return starts
}

// --- unnamed register: delete/yank then paste ---

// captureDelete runs op (which must remove a contiguous chunk from the buffer)
// and stores the removed text in the register. linewise marks the register as
// whole-line content (pasted on its own line by p/P).
func (m *Model) captureDelete(linewise bool, op func()) {
	before := m.ta.Value()
	op()
	removed, ok := removedChunk(before, m.ta.Value())
	if !ok || removed == "" {
		return
	}
	if linewise {
		removed = strings.Trim(removed, "\n")
	}
	m.register = removed
	m.regLinewise = linewise
}

// removedChunk diffs two buffer states where after is before with one
// contiguous chunk removed, and returns that chunk.
func removedChunk(before, after string) (string, bool) {
	if len(after) >= len(before) {
		return "", false
	}
	p := 0
	for p < len(after) && before[p] == after[p] {
		p++
	}
	s := 0
	for s < len(after)-p && before[len(before)-1-s] == after[len(after)-1-s] {
		s++
	}
	return before[p : len(before)-s], true
}

// yankLines copies n lines starting at the cursor into the register (yy, 3yy).
func (m *Model) yankLines(n int) {
	lines := strings.Split(m.ta.Value(), "\n")
	r := m.ta.Line()
	if r < 0 || r >= len(lines) {
		return
	}
	end := r + n
	if end > len(lines) {
		end = len(lines)
	}
	m.setYank(strings.Join(lines[r:end], "\n"), true)
}

// pasteCmd handles p/P: paste the register (count times); with an empty
// register it falls back to the system clipboard via the textarea's paste
// command — propagating the returned tea.Cmd, which a bare synthetic key-send
// would discard.
func (m Model) pasteCmd(after bool) (tea.Model, tea.Cmd) {
	m.pushUndo()
	if m.register == "" {
		m.count = 0
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
		return m, cmd
	}
	for n := m.takeCount(); n > 0; n-- {
		m.pasteRegister(after)
	}
	return m, nil
}

// pasteRegister inserts the register at the cursor (after = Vim's p, else P).
func (m *Model) pasteRegister(after bool) {
	if m.regLinewise {
		if after {
			m.ta.CursorEnd()
			m.ta.InsertString("\n" + m.register)
		} else {
			m.ta.CursorStart()
			m.ta.InsertString(m.register + "\n")
		}
		return
	}
	if after {
		m.arrow(tea.KeyRight)
	}
	m.ta.InsertString(m.register)
}

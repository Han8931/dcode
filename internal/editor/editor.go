// Package editor provides an in-app code editor with a configurable, hand-rolled
// Vim mode built on Bubble Tea + the bubbles textarea component.
//
// Vim is modeled as a small state machine over the textarea:
//
//	Normal  - movement (h/j/k/l), enter Insert (i/a/o), delete char (x),
//	          open the command line (:)
//	Insert  - normal typing; Esc returns to Normal
//	Command - the ":" line; runs submit / w / q (see the design discussion)
//
// Cursor motions in Normal mode are implemented by translating h/j/k/l into the
// arrow-key messages the textarea already understands, so we reuse its cursor
// logic instead of reimplementing it.
//
// When config selects "default" keybindings, the modal layer is bypassed and
// keys are forwarded straight to the textarea.
package editor

import (
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Action is why the editor closed.
type Action string

const (
	ActionSubmit Action = "submit" // ":submit" — check the solution
	ActionSave   Action = "save"   // editor closed after a save (rare; :w stays open)
	ActionQuit   Action = "quit"   // ":q" — leave without checking
)

type mode int

const (
	modeNormal mode = iota
	modeInsert
	modeCommand
	modeVisual
)

// Result is returned to the caller after the editor closes.
type Result struct {
	Action Action
	Code   string
}

// DoneMsg is emitted when the editor reaches a terminal action (submit/quit).
// A standalone editor (Open) turns this into tea.Quit; an embedding parent
// intercepts it to react (e.g. run tests on submit) without killing its program.
type DoneMsg struct{ Action Action }

// OpenConfigMsg is emitted by the ":config" command. The embedding parent
// handles it (e.g. launches the user's $EDITOR on the config file); the editor
// itself stays open in Normal mode.
type OpenConfigMsg struct{}

// RunCommandMsg forwards an ex-command the editor doesn't own (e.g. ":topic go",
// ":progress", ":clear") up to the embedding parent, which dispatches it. Raw is
// the command without the leading colon. This lets the editor's command line
// double as the app's global command line instead of having two of them.
type RunCommandMsg struct {
	Raw string
	// Sel is the editor selection captured when the command was launched with
	// ":" from Visual mode (nil otherwise), so a parent command can scope
	// itself to the selected span and write a result back to it.
	Sel *Selection
}

// Selection is a captured Visual-mode span: its text plus the half-open
// [Start, Cut) flat-rune range it occupied. The range lets a parent apply an
// edit back to exactly that span later (ReplaceRange verifies it first).
type Selection struct {
	Text       string
	Start, Cut int
}

// selSpan is the editor-internal form of a captured selection.
type selSpan struct {
	text       string
	start, cut int
}

// SaveFunc persists an in-progress draft. Called on ":w" while editing.
type SaveFunc func(code string) error

// Model is the editor component. It can run on its own via Open or be embedded
// in a parent Bubble Tea model via New + delegated Update/View.
type Model struct {
	vim    bool
	mode   mode
	ta     textarea.Model
	cmd    textinput.Model
	save   SaveFunc
	status string
	lang   string
	width  int
	height int

	// standalone is set by Open; it makes DoneMsg quit the program. When
	// embedded (New), DoneMsg is left for the parent to handle.
	standalone bool

	// pending holds the first key of a two-key Normal-mode command (operator or
	// prefix): 'd' (delete), 'c' (change), 'g' (goto), 'r' (replace),
	// 'y' (yank), '<'/'>' (dedent/indent). 0 = none.
	pending rune

	// register is Vim's unnamed register: the text captured by the latest
	// delete/yank, pasted by p/P. regLinewise marks whole-line content (dd/yy),
	// which p pastes on its own line instead of inline.
	register    string
	regLinewise bool

	// Visual-mode selection: the anchor is the position where v/V was pressed;
	// the selection spans anchor..cursor. visualLine marks linewise mode (V).
	// visualTop is the hand-rolled Visual view's first visible segment,
	// anchored to the textarea's viewport on entry and scrolled minimally so
	// the view never jumps.
	anchorRow, anchorCol int
	visualLine           bool
	visualTop            int

	// selCapture holds the selection captured when ":" is pressed in Visual
	// mode, carried into the next RunCommandMsg so a parent command (e.g.
	// ":edit make this concise") can act on the selected span. Consumed and
	// cleared when the command line runs or is cancelled.
	selCapture *selSpan

	// undo/redo snapshots ('u' and Ctrl-R in Normal mode). One snapshot is
	// taken before each mutating command, and once on Insert entry so a whole
	// typing session is a single undo unit.
	undoStack []editState
	redoStack []editState

	// count is the numeric prefix being typed (3w, 2dd, 5x); 0 = none.
	// pendingCount preserves it across a two-key command (2dd arms 'd' with 2).
	count        int
	pendingCount int

	// lastFindOp/Ch remember the latest f/F/t/T so ; and , can repeat it.
	lastFindOp rune
	lastFindCh rune

	// jumps is the Vim jumplist: origins recorded by jump commands (G, {, },
	// searches). Ctrl-O walks back through it, Tab (what terminals send for
	// Ctrl-I) forward. jumpIdx == len(jumps) means "at the live position".
	jumps   []jumpPos
	jumpIdx int

	// searchMode marks the command line as a / search; lastSearch feeds n/N.
	searchMode bool
	lastSearch string

	// Command-line history (↑/↓ in the ":" and "/" prompts).
	exHist     CmdHistory
	searchHist CmdHistory

	// Tab completion for the ":" line. globalCmds are the parent's commands
	// (the ones runCommand forwards via RunCommandMsg), completed alongside
	// the editor's own; see WithGlobalCmds. argCandidates (optional) lets the
	// parent complete command ARGUMENTS: given the current input, it returns
	// the full candidate list, or nil to fall back to command names.
	cmdComp       CmdCompleter
	globalCmds    []string
	argCandidates func(input string) []string

	action Action
	done   bool
}

// New builds an embeddable editor pre-filled with starter. vim selects Vim
// bindings; save is invoked on ":w". The caller owns the tea.Program and must
// delegate Update/View and drive sizing via SetSize.
func New(starter string, vim bool, save SaveFunc) Model {
	ta := textarea.New()
	ta.SetValue(starter)
	ta.ShowLineNumbers = true
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.Focus()
	// A steady (non-blinking) cursor: when focused it always renders, so it can
	// never "disappear" mid-blink — the per-mode color (set in View) is always
	// visible.
	ta.Cursor.SetMode(cursor.CursorStatic)
	// SetValue leaves the cursor at the end; start at the top, like opening a
	// file. The textarea ignores keys while blurred, so do this after Focus.
	ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})

	ci := textinput.New()
	ci.Prompt = ":"

	m := Model{
		vim:  vim,
		mode: modeNormal, // Vim opens in Normal; default mode treats this as "typing"
		ta:   ta,
		cmd:  ci,
		save: save,
		lang: "python",
	}
	if !vim {
		m.mode = modeInsert
	}
	return m
}

// WithGlobalCmds registers the parent's ex-commands so the ":" line can
// Tab-complete them too (on Enter they're dispatched via RunCommandMsg).
func (m Model) WithGlobalCmds(names []string) Model {
	m.globalCmds = names
	return m
}

// CmdLineValue returns the ":" line's current input (for tests and parents
// inspecting completion results).
func (m Model) CmdLineValue() string { return m.cmd.Value() }

// WithArgCompleter registers a parent hook that completes command ARGUMENTS
// on the ":" line (e.g. ":topic nos⇥" → the full course id): given the
// current input it returns the candidate list, or nil for the default
// command-name completion.
func (m Model) WithArgCompleter(fn func(input string) []string) Model {
	m.argCandidates = fn
	return m
}

// SetSize lays out the editor within w×h, reserving one row for the status line.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.ta.SetWidth(w)
	if h > 1 {
		m.ta.SetHeight(h - 1)
	}
	m.cmd.Width = w
}

// SetValue replaces the editor's contents (e.g. when switching challenges) and
// moves the cursor to the top, as if opening the file fresh. The undo history
// belongs to the previous buffer, so it is dropped — undo must never resurrect
// another note/challenge's text. The textarea ignores keys while blurred, so
// focus is momentarily restored for the move.
func (m *Model) SetValue(s string) {
	m.ta.SetValue(s)
	m.clearHistory()
	wasFocused := m.ta.Focused()
	m.ta.Focus()
	m.send(tea.KeyCtrlHome)
	if !wasFocused {
		m.ta.Blur()
	}
}

// Value returns the current buffer contents.
func (m Model) Value() string { return m.ta.Value() }

// ReplaceAll swaps the whole buffer for s as a SINGLE undoable edit: it
// snapshots the current text first, so `u` restores it. Unlike SetValue (which
// clears history for a freshly loaded note), this keeps the undo stack — used
// to apply an AI rewrite the learner can revert. The cursor returns to the top.
func (m *Model) ReplaceAll(s string) {
	if m.ta.Value() == s {
		return
	}
	m.pushUndo()
	m.ta.SetValue(s)
	wasFocused := m.ta.Focused()
	m.ta.Focus()
	m.send(tea.KeyCtrlHome)
	if !wasFocused {
		m.ta.Blur()
	}
}

// NormalMode reports whether the editor is in Vim Normal mode with no operator
// or count pending — a safe point for a parent to intercept a leader key (e.g.
// ",n") without stealing a keystroke that belongs to a multi-key Vim command.
func (m Model) NormalMode() bool {
	return m.vim && m.mode == modeNormal && m.pending == 0 && m.count == 0
}

// SetLanguage selects the syntax highlighter used when rendering the buffer.
// Empty keeps the historical default: Python.
func (m *Model) SetLanguage(lang string) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		lang = "python"
	}
	m.lang = lang
}

// SetShowLineNumbers toggles the textarea's number gutter.
func (m *Model) SetShowLineNumbers(on bool) { m.ta.ShowLineNumbers = on }

// Focus gives the editor's textarea keyboard focus; the returned cmd blinks the cursor.
func (m *Model) Focus() tea.Cmd { return m.ta.Focus() }

// Blur removes keyboard focus from the editor's textarea.
func (m *Model) Blur() { m.ta.Blur() }

var (
	hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	// Mode badges in the status line, color-coded so the current mode is
	// obvious at a glance (green = Normal, blue = Insert, amber = plain Edit).
	normalBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("42")).Padding(0, 1)
	insertBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("39")).Padding(0, 1)
	editBadge   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("214")).Padding(0, 1)

	// Cursor colors per mode. The cursor renders as a reverse-video block, so a
	// foreground color here becomes the block's fill: green in Normal, bright
	// magenta in Insert — both always visible (the cursor is static, not blinking).
	normalCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	insertCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	keywordStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	typeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	stringStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	commentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	numberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("215"))
)

// Open launches the editor as its own full-screen program, pre-filled with
// starter, returning the final Result. vim selects Vim bindings; save is
// invoked on ":w".
func Open(starter string, vim bool, save SaveFunc) (Result, error) {
	m := New(starter, vim, save)
	m.standalone = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	out, err := p.Run()
	if err != nil {
		return Result{}, err
	}
	fm := out.(Model)
	return Result{Action: fm.action, Code: fm.ta.Value()}, nil
}

func (m Model) Init() tea.Cmd { return textarea.Blink }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Only honored standalone; an embedding parent drives layout via SetSize.
		if m.standalone {
			m.SetSize(msg.Width, msg.Height)
		}
		return m, nil

	case DoneMsg:
		// Standalone: terminate the program. Embedded: the parent handles it.
		if m.standalone {
			return m, tea.Quit
		}
		return m, nil

	case tea.KeyMsg:
		// Non-Vim mode: forward everything to the textarea; Ctrl-S submits,
		// Ctrl-Q quits. This is the "default" keybindings path.
		if !m.vim {
			switch msg.String() {
			case "ctrl+s":
				return m.finish(ActionSubmit)
			case "ctrl+q":
				return m.finish(ActionQuit)
			case "tab":
				// KeyTab is not a rune key, so the textarea ignores it; indent here.
				m.ta.InsertString(tabIndent)
				return m, nil
			case "enter":
				m.insertNewlineIndented()
				return m, nil
			case "alt+v":
				return m.insertClipboard()
			case "}":
				m.electricClose()
			}
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(msg)
			return m, cmd
		}
		return m.updateVim(msg)
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

// insertClipboard pastes the system clipboard at the cursor (Alt+V).
func (m Model) insertClipboard() (tea.Model, tea.Cmd) {
	s, err := pasteFromSystem()
	if err != nil || s == "" {
		m.status = "clipboard is empty"
		return m, nil
	}
	if m.vim {
		m.pushUndo()
	}
	m.ta.InsertString(s)
	return m, nil
}

// finish records the terminal action and emits a DoneMsg for the program (or
// parent) to act on. It does not call tea.Quit directly, so an embedded editor
// never tears down its parent program.
func (m Model) finish(a Action) (tea.Model, tea.Cmd) {
	m.action = a
	return m, func() tea.Msg { return DoneMsg{Action: a} }
}

// updateVim routes a key according to the current Vim mode.
func (m Model) updateVim(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl-S / Ctrl-Q always work as escape hatches regardless of mode.
	switch msg.String() {
	case "ctrl+s":
		return m.finish(ActionSubmit)
	case "ctrl+q":
		return m.finish(ActionQuit)
	}

	if m.mode != modeCommand {
		// Alt+V pastes the system clipboard at the cursor in any buffer mode;
		// macOS Option+V arrives as "√", claimed only in Normal mode so a √
		// can still be typed into a note. (Cmd+V never reaches the app — the
		// terminal intercepts it and delivers the text as a bracketed paste,
		// handled just below.)
		if msg.String() == "alt+v" || (msg.String() == "√" && m.mode == modeNormal) {
			return m.insertClipboard()
		}
		// A bracketed paste (terminal Cmd+V / middle click) outside Insert
		// mode must land literally in the buffer — fed key-by-key into Normal
		// mode it would execute as Vim commands.
		if msg.Paste && m.mode != modeInsert {
			m.pushUndo()
			m.ta.InsertString(string(msg.Runes))
			return m, nil
		}
	}

	switch m.mode {
	case modeNormal:
		return m.updateNormal(msg)
	case modeVisual:
		return m.updateVisual(msg)
	case modeInsert:
		switch msg.Type {
		case tea.KeyEsc:
			m.mode = modeNormal
			// Vim moves the cursor one left when leaving Insert, so it rests ON
			// the last typed character (and never past the end of the line).
			if _, col := m.cursorPos(); col > 0 {
				m.ta.SetCursor(col - 1)
			}
			return m, nil
		case tea.KeyTab:
			// KeyTab is not a rune key, so the textarea ignores it; indent here.
			m.ta.InsertString(tabIndent)
			return m, nil
		case tea.KeyEnter:
			m.insertNewlineIndented()
			return m, nil
		}
		if msg.String() == "}" {
			m.electricClose() // dedent a bare-indent line before the brace lands
		}
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		return m, cmd
	case modeCommand:
		return m.updateCommand(msg)
	}
	return m, nil
}

// arrow sends a synthetic arrow key to the textarea so we reuse its cursor math.
func (m *Model) arrow(t tea.KeyType) {
	m.ta, _ = m.ta.Update(tea.KeyMsg{Type: t})
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""

	// A prefix/operator is pending (d, c, g, r, f, …): this key completes it.
	if m.pending != 0 {
		op := m.pending
		m.pending = 0
		return m.runPending(op, msg)
	}

	key := msg.String()

	// Numeric prefix: digits accumulate a count for the next command (3w, 2dd).
	// "0" is the line-start motion unless a count is already in progress.
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' || (key == "0" && m.count > 0) {
		m.count = m.count*10 + int(key[0]-'0')
		return m, nil
	}
	if msg.Type == tea.KeyEsc {
		m.count = 0
		return m, nil
	}

	// Jump commands record their origin in the jumplist (Ctrl-O / Ctrl-I),
	// once per command even when a count repeats the motion.
	switch key {
	case "G", "{", "}":
		m.recordJump(m.curJumpPos())
	}

	// Motions are shared with Visual mode; a count repeats them.
	if m.applyMotion(key) {
		for n := m.takeCount() - 1; n > 0; n-- {
			m.applyMotion(key)
		}
		return m, nil
	}

	switch key {
	// --- undo / redo ---
	case "u":
		m.undo()
	case "ctrl+r":
		m.redo()

	// --- jumplist ---
	case "ctrl+o":
		m.jumpBack()
	case "tab": // terminals send Ctrl-I as Tab
		m.jumpForward()

	// --- char find & repeat ---
	case "f", "F", "t", "T":
		m.pendingCount = m.takeCount()
		m.pending = []rune(key)[0]
		return m, nil
	case ";":
		m.repeatFind(false, m.takeCount())
	case ",":
		m.repeatFind(true, m.takeCount())

	// --- line edits ---
	case "J":
		m.pushUndo()
		m.joinLines(m.takeCount())
	case "~":
		m.pushUndo()
		m.toggleCase(m.takeCount())

	// --- search ---
	case "/":
		m.searchMode = true
		m.mode = modeCommand
		m.cmd.Prompt = "/"
		m.cmd.SetValue("")
		m.cmd.Focus()
		m.searchHist.Open()
		return m, textinput.Blink
	case "n":
		if from := m.curJumpPos(); m.search(m.lastSearch, 1) {
			m.recordJump(from)
		} else {
			m.status = "pattern not found: " + m.lastSearch
		}
	case "N":
		if from := m.curJumpPos(); m.search(m.lastSearch, -1) {
			m.recordJump(from)
		} else {
			m.status = "pattern not found: " + m.lastSearch
		}

	// --- enter Visual ---
	case "v":
		m.enterVisual(false)
	case "V":
		m.enterVisual(true)

	// --- enter Insert (one undo unit per session) ---
	case "i":
		m.pushUndo()
		m.mode = modeInsert
	case "I":
		m.pushUndo()
		m.ta.CursorStart()
		m.mode = modeInsert
	case "a":
		m.pushUndo()
		m.arrow(tea.KeyRight)
		m.mode = modeInsert
	case "A":
		m.pushUndo()
		m.ta.CursorEnd()
		m.mode = modeInsert
	case "o":
		// Open below with automatic indentation: the current line's depth, one
		// level deeper when it ends with an opener ({, (, [, :).
		m.pushUndo()
		lines := strings.Split(m.ta.Value(), "\n")
		indent := m.lineIndent()
		if row, _ := m.cursorPos(); row >= 0 && row < len(lines) {
			indent = autoIndentFor(lines[row])
		}
		m.ta.CursorEnd()
		m.send(tea.KeyEnter)
		if indent != "" {
			m.ta.InsertString(indent)
		}
		m.mode = modeInsert
	case "O":
		// Open above at the current line's indentation.
		m.pushUndo()
		indent := m.lineIndent()
		m.ta.CursorStart()
		m.send(tea.KeyEnter)
		m.arrow(tea.KeyUp)
		if indent != "" {
			m.ta.InsertString(indent)
		}
		m.mode = modeInsert

	// --- edits ---
	case "x":
		m.deleteChars(m.takeCount())
	case "D":
		m.pushUndo()
		m.captureDelete(false, func() { m.send(tea.KeyCtrlK) }) // delete to end of line
	case "C":
		m.pushUndo()
		m.captureDelete(false, func() { m.send(tea.KeyCtrlK) })
		m.mode = modeInsert
	case "p":
		return m.pasteCmd(true) // register first; system clipboard as fallback
	case "P":
		return m.pasteCmd(false)

	// --- prefixes / operators ---
	case "d", "c", "g", "r", "y", "<", ">":
		m.pendingCount = m.takeCount()
		m.pending = []rune(key)[0]
		return m, nil

	// --- command line ---
	case ":":
		m.mode = modeCommand
		m.cmd.SetValue("")
		m.cmd.Focus()
		m.exHist.Open()
		m.selCapture = nil // a ":" from Normal mode has no selection
		return m, textinput.Blink
	}
	return m, nil
}

// runPending completes a two-key Normal command begun by op (the pending key)
// using msg as the second key. Unknown combinations are silently ignored, as in
// Vim.
func (m Model) runPending(op rune, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	// Mutating operators snapshot first; undo skips no-op snapshots, so invalid
	// combinations (silently ignored below) leave history untouched.
	switch op {
	case 'd', 'c', 'r', '<', '>':
		m.pushUndo()
	}
	switch op {
	case 'f', 'F', 't', 'T':
		if len(msg.Runes) == 1 {
			n := m.pendingCount
			if n < 1 {
				n = 1
			}
			if !m.findChar(op, msg.Runes[0], n) {
				m.status = "not found: " + string(msg.Runes)
			}
		}
	case 'g':
		if key == "g" {
			m.send(tea.KeyCtrlHome) // gg -> top of buffer
		}
	case 'd':
		switch key {
		case "d":
			n := m.pendingCount
			if n < 1 {
				n = 1
			}
			m.captureDelete(true, func() {
				for i := 0; i < n; i++ {
					m.deleteLine()
				}
			})
		case "w", "e":
			m.captureDelete(false, func() {
				m.send2(tea.KeyMsg{Type: tea.KeyDelete, Alt: true}) // dw -> delete word
			})
		case "$":
			m.captureDelete(false, func() { m.send(tea.KeyCtrlK) })
		case "0", "^":
			m.captureDelete(false, func() { m.send(tea.KeyCtrlU) }) // delete to line start
		}
	case 'c':
		switch key {
		case "c":
			m.captureDelete(true, func() {
				m.ta.CursorStart()
				m.send(tea.KeyCtrlK)
			})
			m.mode = modeInsert
		case "w", "e":
			m.captureDelete(false, func() {
				m.send2(tea.KeyMsg{Type: tea.KeyDelete, Alt: true})
			})
			m.mode = modeInsert
		case "$":
			m.captureDelete(false, func() { m.send(tea.KeyCtrlK) })
			m.mode = modeInsert
		}
	case 'y':
		if key == "y" {
			m.yankLines(max(1, m.pendingCount)) // yy / 3yy -> yank line(s)
		}
	case '<':
		if key == "<" {
			m.indentLines(max(1, m.pendingCount), -1) // << / 3<< -> dedent line(s)
		}
	case '>':
		if key == ">" {
			m.indentLines(max(1, m.pendingCount), 1) // >> / 3>> -> indent line(s)
		}
	case 'r':
		// Replace the character under the cursor with the typed rune.
		if len(msg.Runes) == 1 {
			m.clampCharwise()
			m.send(tea.KeyDelete)
			m.ta, _ = m.ta.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: msg.Runes})
			m.arrow(tea.KeyLeft)
		}
	}
	return m, nil
}

// deleteLine removes the current line (Vim "dd"): clear its content, then pull
// the following line up.
func (m *Model) deleteLine() {
	m.ta.CursorStart()
	m.send(tea.KeyCtrlK)  // delete to end of line
	m.send(tea.KeyDelete) // remove the trailing newline
}

// send forwards a synthetic key (by type) to the textarea, reusing its editing
// logic instead of reimplementing it.
func (m *Model) send(t tea.KeyType) { m.ta, _ = m.ta.Update(tea.KeyMsg{Type: t}) }

// send2 forwards a fully-specified synthetic key (e.g. with Alt) to the textarea.
func (m *Model) send2(k tea.KeyMsg) { m.ta, _ = m.ta.Update(k) }

// cmdHist selects the history matching the active prompt (":" vs "/").
func (m *Model) cmdHist() *CmdHistory {
	if m.searchMode {
		return &m.searchHist
	}
	return &m.exHist
}

func (m Model) updateCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type != tea.KeyTab && msg.Type != tea.KeyShiftTab {
		m.cmdComp.Reset() // any other key ends the completion cycle
	}
	switch msg.Type {
	case tea.KeyTab, tea.KeyShiftTab:
		if m.searchMode {
			return m, nil // "/" searches text; there's nothing to complete
		}
		dir := 1
		if msg.Type == tea.KeyShiftTab {
			dir = -1
		}
		if s, ok := m.cmdComp.Next(m.cmd.Value(), m.exCmdNames(), dir); ok {
			m.cmd.SetValue(s)
			m.cmd.CursorEnd()
		}
		return m, nil
	case tea.KeyUp:
		if s, ok := m.cmdHist().Prev(m.cmd.Value()); ok {
			m.cmd.SetValue(s)
			m.cmd.CursorEnd()
		}
		return m, nil
	case tea.KeyDown:
		if s, ok := m.cmdHist().Next(); ok {
			m.cmd.SetValue(s)
			m.cmd.CursorEnd()
		}
		return m, nil
	case tea.KeyEsc:
		m.mode = modeNormal
		m.cmd.Blur()
		m.cmd.Prompt = ":"
		m.searchMode = false
		m.selCapture = nil // dropping the command line drops the captured selection
		return m, nil
	case tea.KeyEnter:
		m.cmdHist().Record(m.cmd.Value())
		if m.searchMode {
			m.mode = modeNormal
			m.cmd.Blur()
			m.cmd.Prompt = ":"
			m.searchMode = false
			m.lastSearch = m.cmd.Value()
			if from := m.curJumpPos(); m.search(m.lastSearch, 1) {
				m.recordJump(from)
			} else {
				m.status = "pattern not found: " + m.lastSearch
			}
			return m, nil
		}
		return m.runCommand(m.cmd.Value())
	}
	var cmd tea.Cmd
	m.cmd, cmd = m.cmd.Update(msg)
	return m, cmd
}

// editorExCmds are the editor's own ex-commands, for Tab completion. They
// mirror the cases in runCommand; the parent's commands (WithGlobalCmds) are
// completed alongside them.
var editorExCmds = []string{"config", "q", "quit", "submit", "w", "wq", "write"}

// exCmdNames returns the ":" line's completion candidates: the parent's
// argument completions when its hook claims the input, else the editor's
// commands merged with the parent's, sorted so the Tab cycle runs
// alphabetically.
func (m Model) exCmdNames() []string {
	if m.argCandidates != nil {
		if c := m.argCandidates(m.cmd.Value()); c != nil {
			return c
		}
	}
	names := make([]string, 0, len(editorExCmds)+len(m.globalCmds))
	names = append(names, editorExCmds...)
	names = append(names, m.globalCmds...)
	sort.Strings(names)
	return slices.Compact(names) // "config" is in both lists
}

// runCommand dispatches the typed ex-command. ":submit" checks the solution,
// ":w" saves a draft and stays in the editor, ":q" leaves.
func (m Model) runCommand(raw string) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	m.cmd.Blur()
	sel := m.selCapture // captured by ":" from Visual mode; consumed here
	m.selCapture = nil

	switch raw {
	case "submit":
		return m.finish(ActionSubmit)
	case "w", "write":
		if m.save != nil {
			if err := m.save(m.ta.Value()); err != nil {
				m.status = "save failed: " + err.Error()
				return m, nil
			}
		}
		m.status = "saved ✓  (resume later with the same challenge)"
		return m, nil
	case "wq":
		if m.save != nil {
			_ = m.save(m.ta.Value())
		}
		return m.finish(ActionSubmit)
	case "q", "quit":
		return m.finish(ActionQuit)
	case "config":
		// Hand off to the parent to open the config in an external editor.
		return m, func() tea.Msg { return OpenConfigMsg{} }
	default:
		// Not an editor command — forward it to the parent's global dispatcher
		// (e.g. ":topic go", ":progress", ":clear drafts"), carrying any Visual
		// selection captured when ":" opened so commands like ":edit" can scope
		// to it.
		cmd := raw
		return m, func() tea.Msg {
			msg := RunCommandMsg{Raw: cmd}
			if sel != nil {
				msg.Sel = &Selection{Text: sel.text, Start: sel.start, Cut: sel.cut}
			}
			return msg
		}
	}
}

func (m Model) View() string {
	// Visual mode draws its own buffer view: the textarea cannot render a
	// selection, so the editor takes over while one is active.
	if m.vim && m.mode == modeVisual {
		return m.visualView() + "\n" + m.statusLine()
	}
	// Shape the cursor to the current mode so Normal vs Insert is visible at a
	// glance (block vs underline). m is a copy, so this only affects this frame.
	if m.vim && m.mode == modeNormal {
		m.ta.Cursor.Style = normalCursor
	} else {
		m.ta.Cursor.Style = insertCursor
	}
	return m.highlightView(m.ta.View()) + "\n" + m.statusLine()
}

// highlightView styles the rendered buffer for the editor pane. The markdown
// pass needs to know whether rows begin with the textarea's line-number
// gutter, so it can strip it before classifying a row — and never strip real
// digits when the gutter is off.
func (m Model) highlightView(s string) string {
	switch strings.ToLower(m.lang) {
	case "markdown", "md":
		// The buffer scan anchors fence/quote state to real line numbers, so
		// a row's styling never depends on where the viewport is scrolled.
		return highlightMarkdown(s, m.ta.ShowLineNumbers, mdScanBuffer(m.ta.Value()))
	default:
		return highlightSyntax(m.lang, s)
	}
}

// Highlight applies lang's syntax colors to plain source text. Exported so
// other panes (e.g. the chat transcript's fenced code blocks) can reuse the
// editor's highlighter. "plain"/"text" (and "physics") return s unchanged.
func Highlight(lang, s string) string { return highlightSyntax(lang, s) }

func highlightSyntax(lang, s string) string {
	switch strings.ToLower(lang) {
	case "go", "golang":
		return highlightCode(s, goSyntax())
	case "python", "py":
		return highlightCode(s, pythonSyntax())
	case "markdown", "md":
		return highlightMarkdown(s, false, nil)
	case "physics", "plain", "text", "essay", "":
		// Prose passes through untouched: code rules over prose bold ordinary
		// words and punch holes in the textarea's cursor-line background.
		return s
	default:
		// Unknown CODE languages (sql, js, bash, …) get the language-agnostic
		// rules: strings, numbers, and comments color reliably in any syntax;
		// keyword guessing doesn't.
		return highlightCode(s, genericSyntax())
	}
}

type syntaxRules struct {
	keywords     map[string]bool
	types        map[string]bool
	lineComments []string
	blockStart   string
	blockEnd     string
}

func goSyntax() syntaxRules {
	return syntaxRules{
		keywords: words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var"),
		types:    words("any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr true false nil iota"),
		lineComments: []string{
			"//",
		},
		blockStart: "/*",
		blockEnd:   "*/",
	}
}

// genericSyntax colors what every language shares — strings, numbers, and the
// common comment markers — so fences in unknown languages still read as code.
func genericSyntax() syntaxRules {
	return syntaxRules{
		lineComments: []string{"//", "#", "--"},
		blockStart:   "/*",
		blockEnd:     "*/",
	}
}

func pythonSyntax() syntaxRules {
	return syntaxRules{
		keywords: words("False None True and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield"),
		types:    words("bool bytes dict float int list object set str tuple"),
		lineComments: []string{
			"#",
		},
	}
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

func highlightCode(s string, rules syntaxRules) string {
	lines := strings.SplitAfter(s, "\n")
	var b strings.Builder
	inBlock := false
	for _, line := range lines {
		// Never style through the newline: lipgloss treats "text\n" as a
		// two-line block and pads the empty second line to the first's width,
		// injecting phantom spaces that shift the next row sideways.
		nl := ""
		if strings.HasSuffix(line, "\n") {
			nl, line = "\n", line[:len(line)-1]
		}
		b.WriteString(highlightLine(line, rules, &inBlock))
		b.WriteString(nl)
	}
	return b.String()
}

// updateAmbient folds the SGR sequences inside chunk into the ambient state:
// a reset clears it, any other SGR accumulates. The ambient state is what the
// surrounding renderer (the textarea) expects to stay active — most notably
// the cursor-line background — so highlightLine re-asserts it after every
// styled token, whose lipgloss rendering ends in a reset.
func updateAmbient(chunk string, ambient *string) {
	for i := 0; i < len(chunk); {
		end := ansiEscapeEnd(chunk, i)
		if end == i {
			i++
			continue
		}
		seq := chunk[i:end]
		if strings.HasSuffix(seq, "m") { // SGR only; ignore cursor moves etc.
			if seq == "\x1b[0m" || seq == "\x1b[m" {
				*ambient = ""
			} else {
				*ambient += seq
			}
		}
		i = end
	}
}

func highlightLine(line string, rules syntaxRules, inBlock *bool) string {
	var b strings.Builder
	// styled paints a chunk and restores the ambient SGR state its trailing
	// reset wiped out (folding the chunk's own sequences in first).
	ambient := ""
	styled := func(chunk string, style lipgloss.Style) {
		updateAmbient(chunk, &ambient)
		b.WriteString(style.Render(chunk))
		b.WriteString(ambient)
	}
	for i := 0; i < len(line); {
		if *inBlock {
			end := strings.Index(line[i:], rules.blockEnd)
			if end < 0 {
				styled(line[i:], commentStyle)
				return b.String()
			}
			end += i + len(rules.blockEnd)
			styled(line[i:end], commentStyle)
			i = end
			*inBlock = false
			continue
		}

		if raw, word, end, ok := readIdentToken(line, i); ok {
			switch {
			case rules.keywords[word]:
				b.WriteString(renderToken(raw, keywordStyle, &ambient))
			case rules.types[word]:
				b.WriteString(renderToken(raw, typeStyle, &ambient))
			default:
				updateAmbient(raw, &ambient)
				b.WriteString(raw)
			}
			i = end
			continue
		}

		if escEnd := ansiEscapeEnd(line, i); escEnd > i {
			updateAmbient(line[i:escEnd], &ambient)
			b.WriteString(line[i:escEnd])
			i = escEnd
			continue
		}

		if rules.blockStart != "" && strings.HasPrefix(line[i:], rules.blockStart) {
			end := strings.Index(line[i+len(rules.blockStart):], rules.blockEnd)
			if end < 0 {
				styled(line[i:], commentStyle)
				*inBlock = true
				return b.String()
			}
			end += i + len(rules.blockStart) + len(rules.blockEnd)
			styled(line[i:end], commentStyle)
			i = end
			continue
		}

		if isLineComment(line[i:], rules.lineComments) {
			styled(line[i:], commentStyle)
			return b.String()
		}

		if line[i] == '"' || line[i] == '\'' || line[i] == '`' {
			end := stringEnd(line, i)
			styled(line[i:end], stringStyle)
			i = end
			continue
		}

		if isDigit(line[i]) {
			end := numberEnd(line, i)
			styled(line[i:end], numberStyle)
			i = end
			continue
		}

		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

func readIdentToken(s string, i int) (raw, word string, end int, ok bool) {
	var rb, wb strings.Builder
	seenIdent := false
	for j := i; j < len(s); {
		if escEnd := ansiEscapeEnd(s, j); escEnd > j {
			rb.WriteString(s[j:escEnd])
			j = escEnd
			continue
		}
		c := s[j]
		if !isIdentPart(c) {
			word := wb.String()
			if !seenIdent || !isIdentStart(word[0]) {
				return "", "", i, false
			}
			return rb.String(), wb.String(), j, true
		}
		if !seenIdent && !isIdentStart(c) {
			return "", "", i, false
		}
		seenIdent = true
		rb.WriteByte(c)
		wb.WriteByte(c)
		j++
	}
	word = wb.String()
	if !seenIdent || !isIdentStart(word[0]) {
		return "", "", i, false
	}
	return rb.String(), wb.String(), len(s), true
}

// renderToken styles a token's text segments, passing its embedded escape
// sequences through and re-asserting the ambient SGR state after each styled
// segment (whose lipgloss rendering ends in a reset).
func renderToken(raw string, style lipgloss.Style, ambient *string) string {
	var b strings.Builder
	for i := 0; i < len(raw); {
		if escEnd := ansiEscapeEnd(raw, i); escEnd > i {
			updateAmbient(raw[i:escEnd], ambient)
			b.WriteString(raw[i:escEnd])
			i = escEnd
			continue
		}
		j := i + 1
		for j < len(raw) {
			if escEnd := ansiEscapeEnd(raw, j); escEnd > j {
				break
			}
			j++
		}
		b.WriteString(style.Render(raw[i:j]))
		b.WriteString(*ambient)
		i = j
	}
	return b.String()
}

func ansiEscapeEnd(s string, i int) int {
	if i >= len(s) || s[i] != 0x1b {
		return i
	}
	j := i + 1
	if j < len(s) && s[j] == '[' {
		j++
	}
	for ; j < len(s); j++ {
		if s[j] >= '@' && s[j] <= '~' {
			return j + 1
		}
	}
	return len(s)
}

func isLineComment(s string, markers []string) bool {
	for _, marker := range markers {
		if strings.HasPrefix(s, marker) {
			return true
		}
	}
	return false
}

func stringEnd(s string, i int) int {
	quote := s[i]
	j := i + 1
	for j < len(s) {
		if quote != '`' && s[j] == '\\' {
			j += 2
			continue
		}
		if s[j] == quote {
			return j + 1
		}
		j++
	}
	return j
}

func numberEnd(s string, i int) int {
	j := i + 1
	for j < len(s) {
		c := s[j]
		if !(isDigit(c) || isIdentStart(c) || c == '.' || c == '_') {
			break
		}
		j++
	}
	return j
}

func identEnd(s string, i int) int {
	j := i + 1
	for j < len(s) && isIdentPart(s[j]) {
		j++
	}
	return j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool {
	return c == '_' || unicode.IsLetter(rune(c))
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

func (m Model) statusLine() string {
	if m.mode == modeCommand {
		line := m.cmd.View()
		if h := m.cmdComp.Hint(); h != "" {
			line += "   " + hintStyle.Render(h)
		}
		if m.width > 0 {
			line = lipgloss.NewStyle().MaxWidth(m.width).Render(line)
		}
		return line
	}

	var left string
	if m.vim {
		switch m.mode {
		case modeNormal:
			left = normalBadge.Render("NORMAL")
		case modeInsert:
			left = insertBadge.Render("INSERT")
		case modeVisual:
			if m.visualLine {
				left = visualBadge.Render("V-LINE")
			} else {
				left = visualBadge.Render("VISUAL")
			}
		}
	} else {
		left = editBadge.Render("EDIT")
	}

	var hint string
	switch {
	case m.vim && m.mode == modeVisual:
		hint = "  move to select · d/x delete · y yank · c change · </> indent · o swap · esc"
	case m.vim:
		hint = "  :submit  :w  :q  :topic  :progress"
		if m.pending != 0 {
			hint = "  " + string(m.pending) + "…" // operator pending, awaiting motion
		}
	default:
		hint = "  Ctrl-S submit  Ctrl-Q quit"
	}

	line := left + hintStyle.Render(hint)
	if m.status != "" {
		line += "   " + hintStyle.Render(m.status)
	}
	// Keep the status line to a single row: a wrapped line would push the editor
	// past its allotted height and corrupt the surrounding layout.
	if m.width > 0 {
		line = lipgloss.NewStyle().MaxWidth(m.width).Render(line)
	}
	return line
}

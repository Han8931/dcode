package tui

// vault_tui.go is the terminal front-end for the general learning vault. Like the
// web GUI, it is a thin presentation layer over core.Service: a three-pane
// program (notes | editor | chat/study) where all real work — listing notes,
// opening/saving them, generating a lesson, grading an essay, chatting — is done
// by core and this model only renders the result. It reuses the existing
// sidebar/chat/editor components and styles from this package.

import (
	"context"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"dcode/internal/config"
	"dcode/internal/core"
	"dcode/internal/editor"
	"dcode/internal/tutor"
)

// VaultModel is the root model for d-code's terminal UI — the file/notes vault.
type VaultModel struct {
	svc *core.Service

	width, height int
	focus         pane
	exit          SwitchTarget

	// readOnly is set when the open file is source code (read-only): autosave is
	// suspended and edits are not persisted, so the user's source is never
	// rewritten. Notes (markdown) are editable as usual.
	readOnly bool

	// :explain / :decode state. explaining marks the active stream as an
	// explanation; lastExplain holds the last completed explanation text and
	// lastExplainPath the file it was about, so :note can save it as a companion.
	explaining      bool
	lastExplain     string
	lastExplainPath string

	sidebar sidebarModel
	editor  editor.Model
	chat    chatModel

	notes []core.NoteMeta

	// The sidebar's file tree: the vault's real on-disk structure. expanded
	// tracks which directories are unfolded; marked holds the space-bar
	// selection the NERDTree-style batch operations act on.
	tree     []core.TreeEntry
	expanded map[string]bool
	marked   map[string]bool

	// NERDTree-style node-operation state. pendingNode is set after "m" in the
	// sidebar (the next key picks add/move/delete). promptMode tells the
	// status-row input what it is collecting: an ex-command (""), a new node
	// path ("add"), or a rename target ("move", with promptOld the original).
	// confirmDel holds delete targets awaiting a y/n keystroke.
	pendingNode bool
	promptMode  string
	promptOld   string
	confirmDel  []string

	current      string // path of the open note ("" = none)
	currentTitle string
	curPath      *string          // shared with the editor save closure
	chatHist     []tutor.ChatTurn // tutor conversation history

	// Per-note chat contexts: each note keeps its own transcript and tutor
	// conversation, restored when the learner reopens it.
	chatByNote map[string][]chatBlock
	histByNote map[string][]tutor.ChatTurn

	// Streaming chat reply state: one reply at a time.
	streaming      bool
	streamStopping bool
	streamCancel   context.CancelFunc
	streamCh       chan streamChunkMsg

	// AI note-polish state. :polish/:edit stream a proposed rewrite into the
	// chat (polishing == true for that stream); the result is held in
	// pendingEdit until :apply writes it back to the note (or :discard drops
	// it). pendingEditPath records which note the proposal is for, so :apply
	// is a no-op if the learner switched notes meanwhile. When the edit was
	// scoped to a Visual selection, pendingSel holds that span so :apply
	// replaces just it (verifying the span is unchanged); nil = whole note.
	polishing       bool
	pendingEdit     string
	pendingEditPath string
	pendingSel      *editor.Selection

	// cmdSel carries the selection from a ":"-in-Visual command into runEx for
	// the duration of one dispatch; nil outside that window.
	cmdSel *editor.Selection

	// focusExcerpt is a selected passage the learner is discussing with the
	// tutor (:ask): while set, chatContext grounds replies on it. It persists
	// across follow-up questions and clears when another note is opened.
	focusExcerpt string

	// Backlinks ("notes that link here") for the open note, shown as a footer
	// under the editor (Obsidian-style). showBacklinks toggles the panel.
	backlinks     []core.NoteMeta
	showBacklinks bool

	// global ex-command line (":" from the notes pane)
	cmdMode bool
	cmdLine textinput.Model
	cmdHist editor.CmdHistory
	cmdComp editor.CmdCompleter // Tab completion over vaultExCmds

	// Fuzzy finder modal. finderMode is "file" for ,ff and "grep" for ,fg.
	finderMode    string
	finderInput   textinput.Model
	finderCursor  int
	finderResults []finderResult

	// Open-project picker modal (",o" / ":open"). pickerMode is true while it is
	// open; it lists recently opened projects and lets the user type a new path.
	pickerMode    bool
	pickerInput   textinput.Model
	pickerCursor  int
	pickerRecents []string

	// Vim-style chords mirroring the coding TUI: pendingWindow is set after
	// Ctrl-W (the next h/j/k/l picks a pane by direction); pendingLeader is set
	// after "," in the editor's Normal mode (",n" folds the sidebar, ",ff"/",fg"
	// open the fuzzy finder).
	pendingWindow bool
	pendingLeader bool
	pendingFind   bool

	// Chat drag-selection state: a left press on the chat anchors a selection;
	// motion with the button held sweeps it out (Alt-C copies).
	dragChat bool

	// editorBias shifts the editor/chat split (":wide" grows the editor,
	// ":compact" grows the chat), sharing the classic TUI's step/clamp.
	editorBias int

	// sidebarCollapsed hides the notes pane (":fold"); starts from config.
	sidebarCollapsed bool

	// cfg supplies the configured pane ratios and editor keybindings.
	cfg config.Config

	pending  int
	loadKind string
	spin     spinner.Model
	err      error

	// notice is transient command feedback shown in the status bar.
	notice   string
	noticeAt time.Time

	sidebarW, editorW, chatW, contentH int
}

type finderResult struct {
	path    string
	title   string
	context string
}

// RunVault constructs and runs the vault terminal UI over svc. The Outcome tells
// the shell loop (main.runShell) whether to quit or hand off to the coding TUI.
func RunVault(svc *core.Service, cfg config.Config) (Outcome, error) {
	enableTUIColor()
	m := newVaultModel(svc, cfg)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	out := Outcome{}
	if fm, ok := final.(VaultModel); ok {
		out = Outcome{Target: fm.exit}
	}
	return out, err
}

func newVaultModel(svc *core.Service, cfg config.Config) VaultModel {
	vim := cfg.VimEditor()
	curPath := new(string)
	m := VaultModel{
		svc:              svc,
		cfg:              cfg,
		curPath:          curPath,
		showBacklinks:    true,
		sidebarCollapsed: cfg.UI.SidebarFolded,
		sidebar:          newSidebar(),
		chat:             newChat(),
		chatByNote:       map[string][]chatBlock{},
		histByNote:       map[string][]tutor.ChatTurn{},
		expanded:         map[string]bool{vaultRootID: true}, // vault root starts open
		marked:           map[string]bool{},
		spin:             spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
	// The editor's save closure persists the open note (curPath is blanked for
	// read-only buffers so source files are never rewritten).
	save := func(code string) error {
		if *curPath == "" {
			return nil
		}
		_, err := svc.SaveNote(*curPath, code)
		return err
	}
	m.editor = editor.New("", vim, save).WithGlobalCmds(vaultExCmds)
	m.editor.SetLanguage("markdown")
	m.editor.SetShowLineNumbers(cfg.LineNumbers())

	cl := textinput.New()
	cl.Prompt = ":"
	m.cmdLine = cl

	fi := textinput.New()
	fi.Prompt = "› "
	m.finderInput = fi

	pi := textinput.New()
	pi.Prompt = "› "
	pi.Placeholder = "~/code/some-project"
	m.pickerInput = pi

	if m.sidebarCollapsed {
		m.focus = paneEditor
	} else {
		m.focus = paneSidebar
		m.sidebar.focused = true
	}
	return m
}

func (m VaultModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, vListCmd(m.svc))
}

func (m VaultModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		// Mirror in-flight work as an animated progress line inside the chat pane.
		if m.pending > 0 {
			m.chat.setBusy(m.loadKind)
			m.chat.tickBusy()
		} else {
			m.chat.setBusy("")
		}
		return m, cmd

	case vNotesMsg:
		m.notes = msg.notes
		m.tree = msg.tree
		m.rebuildSidebar()
		if msg.truncated {
			m.flash("large project — file tree truncated; add ignores to narrow it")
		}
		return m, nil

	case vOpenedMsg:
		if msg.note.Path != m.pendingEditPath {
			m.pendingEdit, m.pendingSel = "", nil // a proposal doesn't carry to a different note
		}
		if msg.note.Path != m.current {
			m.focusExcerpt = "" // a new note ends the discussion of the old one's excerpt
		}
		m.switchNoteChat(msg.note)
		m.current = msg.note.Path
		m.currentTitle = msg.note.Title
		m.readOnly = msg.note.Source == "code"
		if m.readOnly {
			*m.curPath = "" // source is read-only: never autosave
		} else {
			*m.curPath = msg.note.Path
		}
		m.editor.SetLanguage(langForPath(msg.note.Path, m.readOnly))
		m.editor.SetValue(msg.note.Body)
		m.backlinks = nil         // drop the previous note's backlinks until the fetch returns
		m.expandTo(msg.note.Path) // unfold to a note opened indirectly (:learn, :new)
		m.rebuildSidebar()
		return m, tea.Batch(m.setFocus(paneEditor), vBacklinksCmd(m.svc, m.current))

	case vDeletedMsg:
		for _, p := range msg.paths {
			delete(m.expanded, p)
			// If the open note went with it, clear the editor: keeping a buffer
			// that autosaves would resurrect the file.
			if m.current == p || strings.HasPrefix(m.current, p+"/") {
				m.current, m.currentTitle = "", ""
				*m.curPath = ""
				m.editor.SetValue("")
				m.backlinks = nil
			}
		}
		if len(msg.paths) == 1 {
			m.flash("deleted " + msg.paths[0])
		} else {
			m.flash("deleted " + itoa(len(msg.paths)) + " items")
		}
		return m, vListCmd(m.svc)

	case vRenamedMsg:
		// Carry fold state and the open note across the move.
		if m.expanded[msg.oldPath] {
			delete(m.expanded, msg.oldPath)
			m.expanded[msg.newPath] = true
		}
		switch {
		case m.current == msg.oldPath:
			m.current = msg.newPath
			*m.curPath = m.current
		case strings.HasPrefix(m.current, msg.oldPath+"/"):
			m.current = msg.newPath + strings.TrimPrefix(m.current, msg.oldPath)
			*m.curPath = m.current
		}
		m.expandTo(msg.newPath)
		m.flash("moved to " + msg.newPath)
		return m, vListCmd(m.svc)

	case vMkdirMsg:
		m.expanded[msg.path] = true // show the new directory unfolded
		m.expandTo(msg.path)
		m.flash("created " + msg.path + "/")
		return m, vListCmd(m.svc)

	case vBacklinksMsg:
		if msg.path == m.current {
			m.backlinks = msg.links
			m.layout() // the footer height may have changed
		}
		return m, nil

	case vGeneratedMsg:
		m.pending--
		m.chat.append(roleLesson, "Created note: "+msg.meta.Title)
		return m, tea.Batch(vListCmd(m.svc), vOpenCmd(m.svc, msg.meta.Path))

	case vExplanationSavedMsg:
		m.chat.append(roleOK, "✓ saved explanation → "+msg.meta.Path)
		m.flash("saved " + msg.meta.Path)
		return m, vListCmd(m.svc) // refresh the tree so the new note shows

	case vSavedMsg:
		// Refresh the list in case the title/subject changed; keep editing.
		return m, vListCmd(m.svc)

	case streamChunkMsg:
		return m.handleStreamChunk(msg)

	case vErrMsg:
		m.pending--
		m.err = msg.err
		m.chat.append(roleSystem, "⚠ "+msg.kind+" failed: "+msg.err.Error())
		return m, nil

	case editor.DoneMsg:
		switch msg.Action {
		case editor.ActionSubmit:
			return m.submitEditor()
		case editor.ActionQuit:
			return m, tea.Quit
		}
		return m, nil

	case editor.RunCommandMsg:
		// A command launched with ":" from the editor's Visual mode carries the
		// selected span; stash it so :polish/:edit can scope to it, then clear.
		m.cmdSel = msg.Sel
		tm, cmd := m.runEx(msg.Raw)
		mm := tm.(VaultModel)
		mm.cmdSel = nil
		return mm, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m.forwardToFocus(msg)
}

func (m VaultModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc && m.streaming {
		return m.stopStream()
	}
	if m.finderMode != "" {
		return m.updateFinder(msg)
	}
	if m.pickerMode {
		return m.updatePicker(msg)
	}
	if m.cmdMode {
		return m.updateCmdLine(msg)
	}

	// Ctrl-W starts a window command; the next key chooses a pane by direction
	// (Vim window-style), mirroring the coding TUI.
	if m.pendingWindow {
		m.pendingWindow = false
		switch msg.String() {
		case "h", "left", "k", "up", "shift+tab":
			return m, m.focusDir(-1)
		case "l", "right", "j", "down", "tab", "ctrl+w":
			return m, m.focusDir(1)
		}
		return m, nil // unknown window command: ignore, like Vim
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "ctrl+w":
		// Focus moves via the Vim window chord (Ctrl-W then h/l); bare Tab is left
		// for the panes (e.g. indenting in the editor).
		m.pendingWindow = true
		return m, nil
	}

	// A leader chord lives for exactly one keystroke: clear it here so a stray
	// "," can never carry across a focus change or non-editor key.
	findLeader := m.pendingFind
	m.pendingFind = false
	leader := m.pendingLeader
	m.pendingLeader = false

	// The ",f?" finder chord completes from ANY pane — the finder must be
	// reachable on a fresh vault, where focus starts on the sidebar with no
	// note open.
	if findLeader {
		switch msg.String() {
		case "f":
			return m.openFinder("file")
		case "g":
			return m.openFinder("grep")
		}
		return m, nil
	}

	// ",o" opens the project picker from any pane — like the ",f?" finder chord,
	// it must work on a fresh vault where focus starts on the sidebar.
	if leader && msg.String() == "o" {
		return m.openProjectPicker()
	}

	switch m.focus {
	case paneSidebar:
		// The leader chords work from the sidebar too: ",ff"/",fg" open the
		// finder, ",n" folds the pane — same keys as the editor's Normal mode.
		if leader {
			switch msg.String() {
			case "n":
				return m.cmdFold()
			case "f":
				m.pendingFind = true
			}
			return m, nil
		}
		// A pending delete confirmation eats the next key: y deletes, anything
		// else cancels.
		if len(m.confirmDel) > 0 {
			targets := m.confirmDel
			m.confirmDel = nil
			if msg.String() == "y" {
				m.marked = map[string]bool{}
				m.rebuildSidebar()
				return m, vDeleteCmd(m.svc, targets)
			}
			m.flash("delete cancelled")
			return m, nil
		}
		// The "m" node menu: the next key picks the operation.
		if m.pendingNode {
			m.pendingNode = false
			it, ok := m.sidebar.selected()
			if !ok {
				return m, nil
			}
			switch msg.String() {
			case "a":
				return m.openNodePrompt("add", it)
			case "m":
				if it.root {
					m.flash("the vault root can't be moved")
					return m, nil
				}
				return m.openNodePrompt("move", it)
			case "d":
				if it.root {
					m.flash("the vault root can't be deleted")
					return m, nil
				}
				return m.confirmDelete(it)
			}
			return m, nil
		}
		switch msg.String() {
		case ":":
			m.cmdMode = true
			m.cmdLine.SetValue("")
			m.cmdHist.Open()
			return m, m.cmdLine.Focus()
		case "r":
			// Reload the tree from disk (NERDTree-style refresh), for files
			// changed by another app, git, or :publish.
			m.flash("tree refreshed")
			return m, vListCmd(m.svc)
		case " ":
			// Space-mark the row (NERDTree-style multi-select), then step down
			// so a run of files can be marked in one sweep. The vault root is
			// never markable — just step past it.
			if it, ok := m.sidebar.selected(); ok {
				if !it.root {
					if m.marked[it.id] {
						delete(m.marked, it.id)
					} else {
						m.marked[it.id] = true
					}
					m.rebuildSidebar()
				}
				m.sidebar.move(1)
			}
			return m, nil
		case "m":
			if _, ok := m.sidebar.selected(); ok {
				m.pendingNode = true
			}
			return m, nil
		case ",":
			m.pendingLeader = true
			return m, nil
		}
		var enter bool
		m.sidebar, enter = m.sidebar.Update(msg)
		if enter {
			if it, ok := m.sidebar.selected(); ok {
				if it.dir { // enter folds/unfolds a directory
					if m.expanded[it.id] {
						delete(m.expanded, it.id)
					} else {
						m.expanded[it.id] = true
					}
					m.rebuildSidebar()
					return m, nil
				}
				return m, vOpenCmd(m.svc, it.id)
			}
		}
		return m, nil
	case paneEditor:
		// Leader chord ",n" folds the sidebar — but only in Vim Normal mode, so
		// it never disturbs typing or a pending multi-key Vim command.
		if leader {
			switch msg.String() {
			case "n":
				return m.cmdFold()
			case "f":
				m.pendingFind = true
				return m, nil
			}
			// Not the fold chord: replay the swallowed "," (its Normal-mode
			// repeat-find), then deliver the key that followed it.
			comma := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}}
			tm, _ := m.editor.Update(comma)
			m.editor = tm.(editor.Model)
			tm, cmd := m.editor.Update(msg)
			m.editor = tm.(editor.Model)
			return m, cmd
		}
		if msg.String() == "," && m.editor.NormalMode() {
			m.pendingLeader = true
			return m, nil
		}
		tm, cmd := m.editor.Update(msg)
		m.editor = tm.(editor.Model)
		return m, cmd
	case paneChat:
		switch msg.String() {
		// Copy the tutor's last reply: Alt+O (Linux) / Option+O (macOS, which
		// arrives as "ø"/"Ø" unless the terminal sends Option as Meta).
		case "alt+o", "ø", "Ø":
			m.flash(copyChat(&m.chat, ""))
			return m, nil
		// Copy the mouse drag-selection: Alt+C / Option+C (macOS sends "ç").
		case "alt+c", "ç", "Ç":
			m.flash(copySelection(&m.chat))
			return m, nil
		// Paste the system clipboard into the chat input: Alt+V / Option+V
		// (macOS sends "√" for Option+V). Cmd+V also works — the terminal
		// delivers it as a bracketed paste straight into the input.
		case "alt+v", "√":
			m.flash(pasteChat(&m.chat))
			return m, nil
		}
		// The leader chords work from the chat's Vim Normal mode too (never
		// from Insert, where "," must type a comma).
		if leader {
			switch msg.String() {
			case "n":
				return m.cmdFold()
			case "f":
				m.pendingFind = true
			}
			return m, nil
		}
		if m.chat.inNormal() && msg.String() == "," {
			m.pendingLeader = true
			return m, nil
		}
		// The input's Vim Normal mode (Esc): ":" opens the command line right
		// from the chat; Enter doesn't send while in it.
		if m.chat.inNormal() && msg.String() == ":" {
			m.cmdMode = true
			m.cmdLine.SetValue("")
			m.cmdHist.Open()
			return m, m.cmdLine.Focus()
		}
		if msg.Type == tea.KeyEnter && !m.chat.inNormal() {
			return m.submitChat()
		}
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	}
	return m, nil
}

// focusDir moves focus left (d<0) or right (d>0) between panes, clamped to the
// visible panes — a folded sidebar isn't a target. Mirrors the coding TUI.
func (m *VaultModel) focusDir(d int) tea.Cmd {
	lo := int(paneSidebar)
	if m.sidebarCollapsed {
		lo = int(paneEditor)
	}
	n := int(m.focus) + d
	if n < lo {
		n = lo
	}
	if n > int(paneChat) {
		n = int(paneChat)
	}
	return m.setFocus(pane(n))
}

// handleMouse routes wheel events to the pane under the cursor (scrolling never
// steals focus) and focuses the pane under the cursor on a left click.
func (m VaultModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.cmdMode {
		return m, nil
	}
	p, ok := m.paneAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}

	if tea.MouseEvent(msg).IsWheel() {
		switch p {
		case paneChat:
			var cmd tea.Cmd
			m.chat, cmd = m.chat.Update(msg)
			return m, cmd
		case paneSidebar:
			switch msg.Button {
			case tea.MouseButtonWheelDown:
				m.sidebar.move(1)
			case tea.MouseButtonWheelUp:
				m.sidebar.move(-1)
			}
		case paneEditor:
			tm, cmd := m.editor.Update(msg)
			m.editor = tm.(editor.Model)
			return m, cmd
		}
		return m, nil
	}

	// Left click: focus the pane under the cursor; on the chat it also anchors
	// a text SELECTION, so dragging sweeps out transcript text. RELEASING the
	// drag copies the selection automatically (Alt-C also copies, but many
	// Linux terminals eat Alt-<key> as a menu mnemonic, so release-to-copy is
	// the reliable path). Scrolling stays on the wheel and Ctrl-F/B; the
	// terminal's native bypass still works too — Option+drag on macOS,
	// Shift+drag on Linux skip mouse reporting entirely.
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			m.dragChat = p == paneChat
			if m.dragChat {
				lx, ly := m.chatLocal(msg.X, msg.Y)
				m.chat.startSelect(lx, ly)
			} else {
				m.chat.clearSelect()
			}
			return m, m.setFocus(p)
		}
	case tea.MouseActionMotion:
		if m.dragChat && msg.Button == tea.MouseButtonLeft {
			lx, ly := m.chatLocal(msg.X, msg.Y)
			m.chat.dragSelect(lx, ly)
			return m, nil
		}
	case tea.MouseActionRelease:
		if m.dragChat && m.chat.sel.active { // a real drag, not a bare click
			m.flash(copySelection(&m.chat))
		}
		m.dragChat = false
	}
	return m, nil
}

// chatLocal converts a terminal cell to chat-viewport-local coordinates: past
// the title row, each box's border cell, and the panes left of the chat.
func (m VaultModel) chatLocal(x, y int) (int, int) {
	sidebarSpan := m.sidebarW + 2
	if m.sidebarCollapsed {
		sidebarSpan = 0
	}
	return x - (sidebarSpan + m.editorW + 2 + 1), y - 2
}

// paneAt maps a terminal cell to the pane drawn there: row 0 is the title bar,
// the last row the status bar, and each box adds 2 border columns.
func (m VaultModel) paneAt(x, y int) (pane, bool) {
	if y < 1 || y > m.height-2 {
		return 0, false
	}
	sidebarSpan := m.sidebarW + 2
	if m.sidebarCollapsed {
		sidebarSpan = 0
	}
	switch {
	case x < sidebarSpan:
		return paneSidebar, true
	case x < sidebarSpan+m.editorW+2:
		return paneEditor, true
	default:
		return paneChat, true
	}
}

func (m VaultModel) updateCmdLine(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type != tea.KeyTab && msg.Type != tea.KeyShiftTab {
		m.cmdComp.Reset() // any other key ends the completion cycle
	}
	switch msg.Type {
	case tea.KeyTab, tea.KeyShiftTab:
		if m.promptMode != "" {
			return m, nil // path prompts have no command completion
		}
		dir := 1
		if msg.Type == tea.KeyShiftTab {
			dir = -1
		}
		if s, ok := m.cmdComp.Next(m.cmdLine.Value(), vaultExCmds, dir); ok {
			m.cmdLine.SetValue(s)
			m.cmdLine.CursorEnd()
		}
		return m, nil
	case tea.KeyUp:
		if m.promptMode != "" {
			return m, nil // ex-command history stays out of path prompts
		}
		if s, ok := m.cmdHist.Prev(m.cmdLine.Value()); ok {
			m.cmdLine.SetValue(s)
			m.cmdLine.CursorEnd()
		}
		return m, nil
	case tea.KeyDown:
		if m.promptMode != "" {
			return m, nil
		}
		if s, ok := m.cmdHist.Next(); ok {
			m.cmdLine.SetValue(s)
			m.cmdLine.CursorEnd()
		}
		return m, nil
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEnter:
		raw := strings.TrimSpace(m.cmdLine.Value())
		mode, old := m.promptMode, m.promptOld
		m.closeCmdLine()
		if mode != "" {
			return m.runNodePrompt(mode, old, raw)
		}
		if raw == "" {
			return m, nil
		}
		m.cmdHist.Record(raw)
		return m.runEx(raw)
	case tea.KeyEsc:
		m.closeCmdLine()
		return m, nil
	}
	var cmd tea.Cmd
	m.cmdLine, cmd = m.cmdLine.Update(msg)
	return m, cmd
}

// closeCmdLine shuts the status-row input and restores it to ex-command mode.
func (m *VaultModel) closeCmdLine() {
	m.cmdMode = false
	m.cmdLine.Blur()
	m.cmdLine.Prompt = ":"
	m.promptMode = ""
	m.promptOld = ""
}

func (m VaultModel) openFinder(mode string) (tea.Model, tea.Cmd) {
	m.finderMode = mode
	m.finderCursor = 0
	m.finderInput.SetValue("")
	m.finderInput.CursorEnd()
	m.finderInput.Focus()
	m.refreshFinderResults()
	return m, nil
}

func (m VaultModel) updateFinder(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.finderMode = ""
		m.finderInput.Blur()
		return m, nil
	case tea.KeyEnter:
		if len(m.finderResults) == 0 {
			return m, nil
		}
		if m.finderCursor < 0 || m.finderCursor >= len(m.finderResults) {
			m.finderCursor = 0
		}
		p := m.finderResults[m.finderCursor].path
		m.finderMode = ""
		m.finderInput.Blur()
		m.expandTo(p)
		return m, vOpenCmd(m.svc, p)
	case tea.KeyUp:
		if m.finderCursor > 0 {
			m.finderCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.finderCursor < len(m.finderResults)-1 {
			m.finderCursor++
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.finderInput, cmd = m.finderInput.Update(msg)
	m.refreshFinderResults()
	if m.finderCursor >= len(m.finderResults) {
		m.finderCursor = clampMin(len(m.finderResults)-1, 0)
	}
	return m, cmd
}

func (m *VaultModel) refreshFinderResults() {
	q := strings.TrimSpace(m.finderInput.Value())
	switch m.finderMode {
	case "file":
		m.finderResults = m.findFileResults(q)
	case "grep":
		m.finderResults = m.findGrepResults(q)
	default:
		m.finderResults = nil
	}
	if m.finderCursor >= len(m.finderResults) {
		m.finderCursor = clampMin(len(m.finderResults)-1, 0)
	}
}

func (m VaultModel) findFileResults(q string) []finderResult {
	type scored struct {
		result finderResult
		score  int
	}
	rows := make([]scored, 0, len(m.notes))
	for _, n := range m.notes {
		title := n.Title
		if title == "" {
			title = strings.TrimSuffix(path.Base(n.Path), path.Ext(n.Path))
		}
		score := 0
		if q != "" {
			var ok bool
			score, ok = fuzzyScore(q, title+" "+n.Path)
			if !ok {
				continue
			}
		}
		rows = append(rows, scored{
			result: finderResult{path: n.Path, title: title, context: n.Path},
			score:  score,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return strings.ToLower(rows[i].result.path) < strings.ToLower(rows[j].result.path)
	})
	out := make([]finderResult, 0, min(len(rows), 40))
	for i := 0; i < len(rows) && i < 40; i++ {
		out = append(out, rows[i].result)
	}
	return out
}

func (m VaultModel) findGrepResults(q string) []finderResult {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	var out []finderResult
	for _, meta := range m.notes {
		n, err := m.svc.OpenNote(meta.Path)
		if err != nil {
			continue
		}
		title := n.Title
		if title == "" {
			title = meta.Title
		}
		if title == "" {
			title = strings.TrimSuffix(path.Base(meta.Path), path.Ext(meta.Path))
		}
		for i, line := range strings.Split(n.Body, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || !strings.Contains(strings.ToLower(trimmed), q) {
				continue
			}
			out = append(out, finderResult{
				path:    meta.Path,
				title:   title,
				context: itoa(i+1) + ": " + trimmed,
			})
			if len(out) >= 40 {
				return out
			}
		}
	}
	return out
}

func fuzzyScore(query, candidate string) (int, bool) {
	q := []rune(strings.ToLower(query))
	c := []rune(strings.ToLower(candidate))
	if len(q) == 0 {
		return 0, true
	}
	qi := 0
	score := 0
	streak := 0
	for i, r := range c {
		if qi >= len(q) {
			break
		}
		if r != q[qi] {
			streak = 0
			continue
		}
		score += 10 + streak*4
		if i == 0 || c[i-1] == '/' || c[i-1] == '-' || c[i-1] == '_' || c[i-1] == ' ' {
			score += 6
		}
		streak++
		qi++
	}
	if qi != len(q) {
		return 0, false
	}
	return score - len(c)/8, true
}

// --- open-project picker (",o" / ":open") ---

// openProjectPicker opens the modal that switches the decoded project: a list of
// recently opened projects plus a field to type a new path.
func (m VaultModel) openProjectPicker() (tea.Model, tea.Cmd) {
	m.pickerRecents = config.LoadRecents()
	m.pickerMode = true
	m.pickerCursor = 0
	m.pickerInput.SetValue("")
	m.pickerInput.CursorEnd()
	m.pickerInput.Focus()
	return m, nil
}

// projectCandidates is the picker's current row list: the typed path first (when
// non-empty), then recent projects filtered by the typed text.
func (m VaultModel) projectCandidates() []string {
	q := strings.TrimSpace(m.pickerInput.Value())
	var items []string
	if q != "" {
		items = append(items, q)
	}
	ql := strings.ToLower(q)
	for _, r := range m.pickerRecents {
		if r == q {
			continue // already shown as the typed row
		}
		if q == "" || strings.Contains(strings.ToLower(r), ql) {
			items = append(items, r)
		}
	}
	return items
}

func (m VaultModel) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.pickerMode = false
		m.pickerInput.Blur()
		return m, nil
	case tea.KeyEnter:
		items := m.projectCandidates()
		if len(items) == 0 {
			return m, nil
		}
		if m.pickerCursor < 0 || m.pickerCursor >= len(items) {
			m.pickerCursor = 0
		}
		choice := items[m.pickerCursor]
		m.pickerMode = false
		m.pickerInput.Blur()
		return m.openProject(choice)
	case tea.KeyUp:
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.pickerCursor < len(m.projectCandidates())-1 {
			m.pickerCursor++
		}
		return m, nil
	case tea.KeyCtrlC:
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.pickerInput, cmd = m.pickerInput.Update(msg)
	m.pickerCursor = 0 // typing re-filters; start at the top
	return m, cmd
}

// openProject switches the decoded project to the one at input (a typed path or a
// recent), rebuilding the engine over the new source tree and resetting per-note
// state. The old project's files, notes, and chat are dropped — its notes remain
// on disk and return when it's reopened. Refuses to switch mid-stream.
func (m VaultModel) openProject(input string) (tea.Model, tea.Cmd) {
	if m.streaming {
		m.flash("busy — wait for the reply to finish, then open")
		return m, nil
	}
	abs, err := config.ResolveDir(input)
	if err != nil {
		m.flash("can't open: " + err.Error())
		return m, nil
	}
	newSvc, err := m.svc.Reopen(abs, m.cfg.NotesDir)
	if err != nil {
		m.flash(err.Error())
		return m, nil
	}
	m.pickerRecents, _ = config.AddRecent(newSvc.ProjectRoot())
	m.svc = newSvc
	m.resetForProject()
	m.flash("opened " + newSvc.ProjectName())
	return m, tea.Batch(m.setFocus(paneSidebar), vListCmd(m.svc))
}

// resetForProject clears everything tied to the previous project so a freshly
// opened one starts clean: no open file, an empty editor and chat, and a folded-
// shut tree (root open).
func (m *VaultModel) resetForProject() {
	m.current, m.currentTitle = "", ""
	*m.curPath = ""
	m.readOnly = false
	m.editor.SetValue("")
	m.editor.SetLanguage("markdown")
	m.backlinks = nil
	m.notes = nil
	m.tree = nil
	m.expanded = map[string]bool{vaultRootID: true}
	m.marked = map[string]bool{}
	m.chat = newChat()
	m.chatByNote = map[string][]chatBlock{}
	m.histByNote = map[string][]tutor.ChatTurn{}
	m.chatHist = nil
	m.lastExplain, m.lastExplainPath = "", ""
	m.pendingEdit, m.pendingEditPath, m.pendingSel = "", "", nil
	m.focusExcerpt = ""
	m.confirmDel = nil
	m.pendingNode = false
	m.rebuildSidebar()
	m.layout()
}

// openNodePrompt opens the status-row input for a node operation: "add"
// collects a path for a new note/folder under the cursor's directory; "move"
// collects the destination for a rename, prefilled with the current path.
func (m VaultModel) openNodePrompt(mode string, it sidebarItem) (tea.Model, tea.Cmd) {
	m.promptMode = mode
	m.cmdMode = true
	switch mode {
	case "add":
		base := it.id
		if !it.dir {
			base = path.Dir(it.id)
			if base == "." {
				base = ""
			}
		}
		if base != "" {
			base += "/"
		}
		m.cmdLine.Prompt = "add (end with / for a folder): "
		m.cmdLine.SetValue(base)
	case "move":
		m.promptOld = it.id
		m.cmdLine.Prompt = "move to: "
		m.cmdLine.SetValue(it.id)
	}
	m.cmdLine.CursorEnd()
	return m, m.cmdLine.Focus()
}

// runNodePrompt executes a completed node prompt: "add" creates a folder
// (trailing "/") or a markdown note (opened right away); "move" renames.
func (m VaultModel) runNodePrompt(mode, old, raw string) (tea.Model, tea.Cmd) {
	if raw == "" {
		return m, nil
	}
	switch mode {
	case "add":
		if strings.HasSuffix(raw, "/") {
			return m, vMkdirCmd(m.svc, strings.TrimSuffix(raw, "/"))
		}
		if !strings.HasSuffix(strings.ToLower(raw), ".md") {
			raw += ".md"
		}
		m.expandTo(raw)
		return m, vSaveOpenCmd(m.svc, raw, "")
	case "move":
		if raw == old {
			return m, nil
		}
		return m, vRenameCmd(m.svc, old, raw)
	}
	return m, nil
}

// confirmDelete arms the y/n confirmation for the space-marked rows, or for
// the cursor row when nothing is marked. The question renders in the status
// bar until the next key answers it.
func (m VaultModel) confirmDelete(it sidebarItem) (tea.Model, tea.Cmd) {
	targets := make([]string, 0, len(m.marked))
	for p := range m.marked {
		targets = append(targets, p)
	}
	sort.Strings(targets)
	if len(targets) == 0 {
		targets = []string{it.id}
	}
	m.confirmDel = targets
	return m, nil
}

// vaultExCmds lists every command runEx accepts (aliases included), sorted,
// for Tab completion in the command prompt.
var vaultExCmds = []string{
	"apply", "ask", "backlinks", "compact", "copy", "decode", "discard", "discuss",
	"edit", "explain", "export", "fold", "gen", "learn", "lesson", "links", "new",
	"note", "open", "paste", "polish", "q", "quit", "sidebar", "wide", "yank",
}

// runEx dispatches a vault ex-command (without the leading colon).
func (m VaultModel) runEx(raw string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return m, nil
	}
	args := strings.TrimSpace(strings.TrimPrefix(raw, fields[0]))
	switch fields[0] {
	case "learn", "gen", "lesson":
		if args == "" {
			m.flash("usage: :learn <what you want to learn>")
			return m, nil
		}
		m.pending++
		m.loadKind = "generating lesson"
		m.chat.append(roleSystem, "▶ generating a lesson on "+args+"…")
		return m, vGenCmd(m.svc, args)
	case "new":
		if args == "" {
			m.flash("usage: :new <note title>")
			return m, nil
		}
		path := args + ".md"
		return m, vSaveOpenCmd(m.svc, path, "# "+args+"\n\n")
	case "open":
		if args == "" {
			return m.openProjectPicker() // ":open" alone opens the picker
		}
		return m.openProject(args)
	case "fold", "sidebar":
		return m.cmdFold()
	case "compact":
		return m.cmdResizeEditor(-editorBiasStep)
	case "wide":
		return m.cmdResizeEditor(editorBiasStep)
	case "polish":
		return m.cmdPolish(args) // args optional: a one-off instruction, else the default
	case "edit":
		if args == "" {
			m.flash("usage: :edit <what to change> (e.g. :edit make this more concise)")
			return m, nil
		}
		return m.cmdPolish(args)
	case "explain", "decode":
		return m.cmdExplain(args)
	case "note":
		return m.cmdSaveExplanation()
	case "apply":
		return m.cmdApplyEdit()
	case "discard":
		return m.cmdDiscardEdit()
	case "ask", "discuss":
		return m.cmdAsk(args)
	case "copy", "yank":
		what := ""
		if len(fields) > 1 {
			what = fields[1]
		}
		m.flash(copyChat(&m.chat, what))
		return m, nil
	case "export":
		m.flash(exportChat(&m.chat, m.cfg.ExportsDir, m.currentTitle))
		return m, nil
	case "paste":
		m.flash(pasteChat(&m.chat))
		return m, m.setFocus(paneChat) // land where the pasted text is
	case "backlinks", "links":
		m.showBacklinks = !m.showBacklinks
		m.layout()
		if m.showBacklinks {
			m.flash("backlinks panel on")
			if m.current != "" {
				return m, vBacklinksCmd(m.svc, m.current)
			}
		} else {
			m.flash("backlinks panel off")
		}
		return m, nil
	case "q", "quit":
		return m, tea.Quit
	default:
		m.flash("unknown command: :" + raw +
			"  (try :explain · :decode · :note · :ask · :polish · :new · :open · :backlinks · :fold · :q)")
		return m, nil
	}
}

// cmdFold toggles the notes pane. Folding the focused pane moves focus to the
// editor so keys never vanish into a hidden pane.
func (m VaultModel) cmdFold() (tea.Model, tea.Cmd) {
	m.sidebarCollapsed = !m.sidebarCollapsed
	var cmd tea.Cmd
	if m.sidebarCollapsed && m.focus == paneSidebar {
		cmd = m.setFocus(paneEditor)
	}
	m.layout()
	if m.sidebarCollapsed {
		m.flash("Notes pane folded — :fold to bring it back")
	}
	return m, cmd
}

// cmdResizeEditor nudges the editor/chat split by delta percentage points
// (":compact" grows the chat, ":wide" grows the editor), clamps, and re-flows.
func (m VaultModel) cmdResizeEditor(delta int) (tea.Model, tea.Cmd) {
	prev := m.editorBias
	m.editorBias = clampRange(m.editorBias+delta, -editorBiasMax, editorBiasMax)
	if m.editorBias == prev {
		edge := "widest"
		if delta < 0 {
			edge = "narrowest"
		}
		m.flash("Editor already at its " + edge + " — chat can't go further")
		return m, nil
	}
	m.layout()
	switch {
	case m.editorBias < 0:
		m.flash("Editor narrowed — more room for chat (:wide to grow it back)")
	case m.editorBias > 0:
		m.flash("Editor widened (:compact to give chat more room)")
	default:
		m.flash("Editor/chat split reset to default")
	}
	return m, nil
}

// submitEditor handles Ctrl-S / :submit: it saves the open note.
func (m VaultModel) submitEditor() (tea.Model, tea.Cmd) {
	if m.current == "" {
		return m, nil
	}
	return m, vSaveCmd(m.svc, m.current, m.editor.Value())
}

// flash shows transient feedback in the status bar for a few seconds.
func (m *VaultModel) flash(s string) {
	if s == "" {
		return
	}
	m.notice = s
	m.noticeAt = time.Now()
}

// switchNoteChat swaps the chat pane to the opened note's own transcript and
// tutor conversation, saving the outgoing note's first. Reopening a note brings
// its past study activity back; a first visit starts a clean pane with a header.
func (m *VaultModel) switchNoteChat(n core.Note) {
	if n.Path == m.current {
		return
	}
	if m.current != "" {
		m.chatByNote[m.current] = m.chat.snapshot()
		m.histByNote[m.current] = m.chatHist
	}
	saved, visited := m.chatByNote[n.Path]
	m.chat.restore(saved)
	m.chatHist = m.histByNote[n.Path]
	if !visited {
		m.chat.append(roleSystem, "— "+n.Title+" —")
	}
}

// chatContext describes what the learner is looking at — the open note or file —
// so chat replies stay grounded in the current material.
func (m VaultModel) chatContext() string {
	var b strings.Builder
	// Lead with the focused excerpt (the subject of a :ask/:discuss thread) so
	// it ALWAYS survives context clamping — the note body that follows may be
	// trimmed, but the excerpt the learner is asking about must not be.
	if m.focusExcerpt != "" {
		b.WriteString("The learner has SELECTED this excerpt and wants the whole conversation " +
			"focused on it. Keep grounding every reply in it:\n```\n" + m.focusExcerpt + "\n```\n\n")
	}
	if m.current == "" {
		return strings.TrimSpace(b.String())
	}
	b.WriteString("Current note — " + m.currentTitle + "\n")
	b.WriteString("\nNote content (as in the learner's editor):\n" + m.editor.Value())
	return strings.TrimSpace(b.String())
}

// submitChat sends the chat input to the tutor, streaming the reply into the
// transcript, grounded in the open file or note.
func (m VaultModel) submitChat() (tea.Model, tea.Cmd) {
	if m.streaming {
		m.flash("the tutor is still replying — one question at a time")
		return m, nil
	}
	text, ok := m.chat.submit()
	if !ok {
		return m, nil
	}
	m.chat.append(roleUser, text)
	m.chatHist = append(m.chatHist, tutor.ChatTurn{Role: "user", Content: text})
	return m.streamReply()
}

// cmdAsk routes a selected passage into the chat to discuss it with the tutor.
// From the editor's Visual mode, ":ask" (or ":discuss") grounds the
// conversation on the selection — it stays the topic across follow-ups until
// another note is opened. With a trailing question (":ask why is this vague?")
// the question is sent right away; without one, focus moves to the chat input
// so the learner can type. Polishing it instead is just :polish/:edit on the
// same selection.
func (m VaultModel) cmdAsk(question string) (tea.Model, tea.Cmd) {
	if m.streaming {
		m.flash("the tutor is still replying — try again in a moment")
		return m, nil
	}
	question = strings.TrimSpace(question)
	if m.cmdSel != nil && strings.TrimSpace(m.cmdSel.Text) != "" {
		m.focusExcerpt = m.cmdSel.Text
		preview := firstLine(m.focusExcerpt)
		if len(preview) > 50 {
			preview = preview[:50] + "…"
		}
		m.chat.append(roleSystem, "— discussing this excerpt: \""+preview+"\" — ask away (it stays the topic until you open another note) —")
	} else if question == "" && m.focusExcerpt == "" {
		m.flash("select text in the editor first, or :ask <your question>")
		return m, nil
	}
	if question == "" {
		return m, m.setFocus(paneChat) // let the learner type their question
	}
	m.chat.append(roleUser, question)
	m.chatHist = append(m.chatHist, tutor.ChatTurn{Role: "user", Content: question})
	tm, cmd := m.streamReply()
	mm := tm.(VaultModel)
	return mm, tea.Batch(mm.setFocus(paneChat), cmd)
}

// groundHistory returns a copy of the chat history with a :ask/:discuss
// excerpt pinned to the LATEST user turn. The selection rides in the system
// context too, but small models drift away from far-off system text after a
// few turns; keeping it adjacent to the current question is what holds the
// discussion on the selected passage. The input slice and m.chatHist are left
// unchanged (the transcript stays clean).
func groundHistory(hist []tutor.ChatTurn, excerpt string) []tutor.ChatTurn {
	out := append([]tutor.ChatTurn(nil), hist...)
	if excerpt == "" || len(out) == 0 || out[len(out)-1].Role != "user" {
		return out
	}
	out[len(out)-1].Content = "About the excerpt I selected:\n```\n" +
		excerpt + "\n```\n\n" + out[len(out)-1].Content
	return out
}

// streamReply starts streaming one note-grounded tutor reply over the active
// conversation.
func (m VaultModel) streamReply() (tea.Model, tea.Cmd) {
	m.pending++
	m.loadKind = "tutor thinking"
	m.streaming = true
	m.streamStopping = false
	m.chat.beginStream()

	svc := m.svc
	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel
	ctxText := m.chatContext()
	hist := groundHistory(m.chatHist, m.focusExcerpt)
	ch, cmd := startChatStream(ctx, func(ctx context.Context, onDelta func(string)) (string, error) {
		return svc.ChatStream(ctx, ctxText, hist, onDelta)
	})
	m.streamCh = ch
	return m, cmd
}

func (m VaultModel) stopStream() (tea.Model, tea.Cmd) {
	if !m.streaming {
		return m, nil
	}
	if m.streamCancel != nil {
		m.streamCancel()
	}
	m.streamStopping = true
	m.loadKind = "stopping tutor"
	m.chat.append(roleSystem, "— stopping tutor reply —")
	return m, nil
}

// handleStreamChunk advances a streaming tutor reply.
func (m VaultModel) handleStreamChunk(msg streamChunkMsg) (tea.Model, tea.Cmd) {
	if m.streamStopping {
		if msg.done || msg.err != nil {
			m.pending--
			m.streaming = false
			m.streamStopping = false
			m.polishing = false
			m.explaining = false
			m.streamCancel = nil
			m.chat.append(roleSystem, "— tutor reply stopped —")
			return m, nil
		}
		return m, listenStream(m.streamCh)
	}
	if msg.err != nil {
		m.pending--
		m.streaming = false
		m.polishing = false
		m.explaining = false
		m.streamCancel = nil
		m.chat.failStream("⚠ chat failed: " + msg.err.Error())
		return m, nil
	}
	if msg.done {
		m.pending--
		m.streaming = false
		m.streamCancel = nil
		if m.polishing {
			m.polishing = false
			m.pendingEdit = msg.full
			m.chat.append(roleSystem, "— proposed edit ready · :apply to replace the note · :discard to drop —")
			m.flash("proposed edit ready — :apply or :discard")
			return m, nil
		}
		if m.explaining {
			m.explaining = false
			m.lastExplain = msg.full
			// Seed the conversation so follow-up questions stay grounded in the
			// explanation (the open file rides along via chatContext).
			m.chatHist = append(m.chatHist,
				tutor.ChatTurn{Role: "user", Content: "Explain " + m.currentTitle + "."},
				tutor.ChatTurn{Role: "assistant", Content: msg.full})
			m.chat.append(roleSystem, "— explanation ready · :note to save it as a companion note · ask a follow-up below —")
			m.flash(":note to save this explanation")
			return m, nil
		}
		m.chatHist = append(m.chatHist, tutor.ChatTurn{Role: "assistant", Content: msg.full})
		return m, nil
	}
	m.chat.appendStream(msg.delta)
	return m, listenStream(m.streamCh)
}

// --- :explain / :decode — the headline code-decoding flow ---

// cmdExplain streams an explanation of the open file (or a Visual selection) into
// the chat, grounded in the whole file and related project files. The completed
// text is held in lastExplain so :note can save it as a companion note.
func (m VaultModel) cmdExplain(_ string) (tea.Model, tea.Cmd) {
	switch {
	case m.current == "":
		m.flash("open a file first — :explain decodes the open file (or a selection)")
		return m, nil
	case m.svc.Offline():
		m.flash("explaining needs an AI provider — run `dcode check`, then :explain")
		return m, nil
	case m.streaming:
		m.flash("busy — try :explain again in a moment")
		return m, nil
	}

	req := core.ExplainRequest{Path: m.current, Lang: langForPath(m.current, m.readOnly)}
	target := "file"
	if m.cmdSel != nil && strings.TrimSpace(m.cmdSel.Text) != "" {
		req.Selection = m.cmdSel.Text
		target = "selection"
	}

	m.pending++
	m.loadKind = "decoding"
	m.streaming = true
	m.streamStopping = false
	m.explaining = true
	m.lastExplainPath = m.current
	m.chat.append(roleSystem, "▶ decoding the "+target+" — "+m.currentTitle)
	m.chat.beginStream()

	svc := m.svc
	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel
	ch, cmd := startChatStream(ctx, func(ctx context.Context, onDelta func(string)) (string, error) {
		return svc.ExplainStream(ctx, req, onDelta)
	})
	m.streamCh = ch
	return m, tea.Batch(m.setFocus(paneChat), cmd)
}

// cmdSaveExplanation saves the last explanation as a companion markdown note,
// mirroring the source path under the separate notes dir.
func (m VaultModel) cmdSaveExplanation() (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.lastExplain) == "" {
		m.flash("nothing to save yet — :explain a file first, then :note")
		return m, nil
	}
	return m, vSaveExplanationCmd(m.svc, m.lastExplainPath, m.lastExplain)
}

// --- :polish / :edit — AI note editing ---

// cmdPolish streams an AI rewrite of the open note into the chat for review.
// instruction is free-form (":edit make this concise") or empty (":polish",
// which uses core.DefaultPolishInstruction). The result waits in pendingEdit
// until :apply writes it back, so nothing is changed until the learner agrees.
func (m VaultModel) cmdPolish(instruction string) (tea.Model, tea.Cmd) {
	switch {
	case m.current == "":
		m.flash("open a note first — :polish edits the open note")
		return m, nil
	case m.readOnly:
		m.flash("this is read-only source — :polish edits notes, not code (try :explain)")
		return m, nil
	case m.svc.Offline():
		m.flash("polishing needs an AI provider — set one up, then :polish")
		return m, nil
	case m.streaming:
		m.flash("busy — try :polish again in a moment")
		return m, nil
	case m.pendingEdit != "":
		m.flash("you already have a proposed edit — :apply or :discard it first")
		return m, nil
	}
	m.pending++
	m.loadKind = "polishing"
	m.streaming = true
	m.streamStopping = false
	m.polishing = true
	m.pendingEditPath = m.current

	// Scope to the Visual selection when the command came from one; otherwise
	// the whole note.
	body := m.editor.Value()
	target := "note"
	m.pendingSel = nil
	if m.cmdSel != nil && strings.TrimSpace(m.cmdSel.Text) != "" {
		body = m.cmdSel.Text
		sel := *m.cmdSel
		m.pendingSel = &sel
		target = "selection"
	}
	verb := "polishing"
	if strings.TrimSpace(instruction) != "" {
		verb = "editing"
	}
	m.chat.append(roleSystem, "▶ "+verb+" the "+target+" — :apply to use the result · :discard to drop it")
	m.chat.beginStream()

	svc, instr := m.svc, instruction
	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel
	ch, cmd := startChatStream(ctx, func(ctx context.Context, onDelta func(string)) (string, error) {
		return svc.PolishNote(ctx, body, instr, onDelta)
	})
	m.streamCh = ch
	return m, tea.Batch(m.setFocus(paneChat), cmd)
}

// cmdApplyEdit writes the pending AI rewrite back into the editor as one
// undoable change (u reverts it) and saves the note.
func (m VaultModel) cmdApplyEdit() (tea.Model, tea.Cmd) {
	if m.pendingEdit == "" {
		m.flash("nothing to apply — :polish or :edit first")
		return m, nil
	}
	if m.current != m.pendingEditPath {
		m.pendingEdit, m.pendingSel = "", nil
		m.flash("the proposed edit was for another note — discarded")
		return m, nil
	}
	if m.pendingSel != nil {
		// Selection edit: replace just that span, and only if it's unchanged.
		if !m.editor.ReplaceRange(m.pendingSel.Start, m.pendingSel.Cut, m.pendingEdit, m.pendingSel.Text) {
			m.pendingEdit, m.pendingSel = "", nil
			m.flash("the note changed under the selection — edit discarded")
			return m, nil
		}
	} else {
		m.editor.ReplaceAll(m.pendingEdit)
	}
	body := m.editor.Value()
	m.pendingEdit, m.pendingSel = "", nil
	m.chat.append(roleSystem, "— edit applied · press u in the editor to undo —")
	m.flash("note updated — u to undo")
	return m, tea.Batch(m.setFocus(paneEditor), vSaveCmd(m.svc, m.current, body))
}

// cmdDiscardEdit drops a pending AI rewrite without touching the note.
func (m VaultModel) cmdDiscardEdit() (tea.Model, tea.Cmd) {
	if m.pendingEdit == "" {
		m.flash("no proposed edit to discard")
		return m, nil
	}
	m.pendingEdit, m.pendingSel = "", nil
	m.chat.append(roleSystem, "— proposed edit discarded —")
	m.flash("edit discarded")
	return m, nil
}

func (m VaultModel) forwardToFocus(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.cmdMode {
		var cmd tea.Cmd
		m.cmdLine, cmd = m.cmdLine.Update(msg)
		return m, cmd
	}
	switch m.focus {
	case paneEditor:
		tm, cmd := m.editor.Update(msg)
		m.editor = tm.(editor.Model)
		return m, cmd
	case paneChat:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *VaultModel) setFocus(p pane) tea.Cmd {
	m.editor.Blur()
	m.chat.blur()
	m.sidebar.focused = false
	m.focus = p
	switch p {
	case paneEditor:
		return m.editor.Focus()
	case paneChat:
		return m.chat.focus()
	case paneSidebar:
		m.sidebar.focused = true
	}
	return nil
}

// rebuildSidebar groups notes by subject into selectable rows (id = note path).
func (m *VaultModel) rebuildSidebar() {
	// Group entries under their parent directory, then walk the tree depth-
	// first from the root, emitting only rows whose ancestors are expanded.
	// Directories sort before files at each level, NERDTree/Obsidian-style.
	children := map[string][]core.TreeEntry{}
	for _, e := range m.tree {
		parent := path.Dir(e.Path)
		if parent == "." {
			parent = ""
		}
		children[parent] = append(children[parent], e)
	}
	for _, ents := range children {
		sort.SliceStable(ents, func(i, j int) bool {
			if ents[i].Dir != ents[j].Dir {
				return ents[i].Dir
			}
			return strings.ToLower(ents[i].Name) < strings.ToLower(ents[j].Name)
		})
	}

	var items []sidebarItem
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		for _, e := range children[dir] {
			items = append(items, sidebarItem{
				id:       e.Path,
				title:    e.Name,
				depth:    depth,
				dir:      e.Dir,
				expanded: m.expanded[e.Path],
				marked:   m.marked[e.Path],
				active:   !e.Dir && e.Path == m.current,
			})
			if e.Dir && m.expanded[e.Path] {
				walk(e.Path, depth+1)
			}
		}
	}
	// The vault root is always shown first (NERDTree-style): a directory you
	// can add notes into — the default home for new notes — but never move,
	// delete, or mark. Its real entries nest one level under it. Fold state
	// lives under the "" key in m.expanded (seeded open in newVaultModel).
	rootOpen := m.expanded[vaultRootID]
	items = append(items, sidebarItem{
		id:       vaultRootID,
		title:    vaultRootLabel,
		dir:      true,
		root:     true,
		expanded: rootOpen,
	})
	if rootOpen {
		walk("", 1)
	}
	m.sidebar.setItems(items)
}

// vaultRootID is the synthetic sidebar id (and m.expanded key) of the vault-
// root row. The empty path is the vault root and can never name a real entry.
const vaultRootID = ""

// vaultRootLabel is the display name of the vault-root sidebar row. It is a
// fixed generic label on purpose — NOT the configured directory's base name —
// so the learner's real (possibly personal) vault path never shows on screen.
const vaultRootLabel = "vault"

// expandTo unfolds every ancestor directory of relPath so its row is visible
// after the next sidebar rebuild.
func (m *VaultModel) expandTo(relPath string) {
	for d := path.Dir(relPath); d != "." && d != "/" && d != ""; d = path.Dir(d) {
		m.expanded[d] = true
	}
}

// --- layout & view ---

func (m *VaultModel) layout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.cmdLine.Width = m.width - 4
	m.contentH = m.height - 4
	if m.contentH < 1 {
		m.contentH = 1
	}
	borders := 6 // three bordered boxes, 2 columns each
	if m.sidebarCollapsed {
		borders = 4
	}
	contentW := m.width - borders
	if contentW < 3 {
		contentW = 3
	}
	// The configured split is the base; :compact / :wide shift it live.
	chatPct := clampRange(m.cfg.ChatPct(32)-m.editorBias, 15, 75)
	if m.sidebarCollapsed {
		m.sidebarW = 0
		m.chatW = clampMin(contentW*chatPct/100, 16)
		m.editorW = clampMin(contentW-m.chatW, 10)
	} else {
		m.sidebarW = clampMin(contentW*m.cfg.SidebarPct(22)/100, 12)
		m.chatW = clampMin(contentW*chatPct/100, 16)
		m.editorW = clampMin(contentW-m.sidebarW-m.chatW, 10)
	}

	m.sidebar.setSize(m.sidebarW, m.contentH)
	m.finderInput.Width = max(20, m.width-18)
	reserved := 1 + len(m.backlinkFooterLines(m.editorW))
	m.editor.SetSize(m.editorW, max(1, m.contentH-reserved))
	m.chat.setSize(m.chatW, m.contentH)
}

func (m VaultModel) View() string {
	if m.width == 0 {
		return "starting…"
	}
	if m.width < 60 || m.height < 16 {
		return "Terminal too small — please enlarge to at least 60×16."
	}
	ed := m.box(paneEditor, m.editorW, m.contentH, m.editorPaneView(m.editorW))
	ch := m.box(paneChat, m.chatW, m.contentH, m.chat.view())
	var row string
	if m.sidebarCollapsed {
		row = lipgloss.JoinHorizontal(lipgloss.Top, ed, ch)
	} else {
		sb := m.box(paneSidebar, m.sidebarW, m.contentH, m.sidebar.view())
		row = lipgloss.JoinHorizontal(lipgloss.Top, sb, ed, ch)
	}
	frame := lipgloss.JoinVertical(lipgloss.Left, m.titleView(), row, m.statusView())
	frame = lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(frame)
	if m.finderMode != "" {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.finderView())
	}
	if m.pickerMode {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.pickerView())
	}
	return frame
}

func (m VaultModel) box(p pane, w, h int, content string) string {
	return borderStyle(m.focus == p).
		Width(w).Height(h).
		MaxWidth(w + 2).MaxHeight(h + 2).
		Render(content)
}

func (m VaultModel) editorPaneView(w int) string {
	label := "No file open"
	if m.current != "" {
		if m.readOnly {
			label = "CODE · " + m.currentTitle + " · read-only"
		} else {
			label = "NOTE · " + m.currentTitle
		}
	}
	if w > 0 && lipgloss.Width(label) > w-2 {
		label = truncate(label, max(1, w-2))
	}
	out := editorHeader.Width(w).Render(label)
	out += "\n" + m.editor.View()
	for _, ln := range m.backlinkFooterLines(w) {
		out += "\n" + ln
	}
	return out
}

func (m VaultModel) finderView() string {
	title := "Find files"
	hint := ",ff · type to filter · enter open · esc close"
	empty := "no notes"
	if m.finderMode == "grep" {
		title = "Find contents"
		hint = ",fg · type to search contents · enter open · esc close"
		empty = "type to search note contents"
	}

	w := clampRange(m.width-10, 40, 92)
	if m.width < 50 {
		w = clampMin(m.width-4, 24)
	}
	inner := clampMin(w-6, 12)
	maxRows := min(len(m.finderResults), 12)

	var b strings.Builder
	b.WriteString(titleBar.Render(" " + title + " "))
	b.WriteString("\n\n")
	b.WriteString(m.finderInput.View())
	b.WriteString("\n\n")
	if len(m.finderResults) == 0 {
		b.WriteString(hintStyle.Render(empty))
	} else {
		for i := 0; i < maxRows; i++ {
			r := m.finderResults[i]
			label := r.title
			if r.context != "" {
				label += "  " + r.context
			}
			label = truncate(label, inner-2)
			if i == m.finderCursor {
				b.WriteString(selectedRow.Width(inner).Render("▸ " + label))
			} else {
				b.WriteString("  " + label)
			}
			if i < maxRows-1 {
				b.WriteString("\n")
			}
		}
		if len(m.finderResults) > maxRows {
			b.WriteString("\n" + hintStyle.Render("  +"+itoa(len(m.finderResults)-maxRows)+" more"))
		}
	}
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render(hint))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39")).
		Padding(1, 2).
		Width(w).
		Render(b.String())
}

// pickerView renders the open-project modal: a path field over the list of
// recent projects (filtered by what's typed). It mirrors finderView's framing.
func (m VaultModel) pickerView() string {
	w := clampRange(m.width-10, 40, 92)
	if m.width < 50 {
		w = clampMin(m.width-4, 24)
	}
	inner := clampMin(w-6, 12)
	items := m.projectCandidates()
	maxRows := min(len(items), 12)

	var b strings.Builder
	b.WriteString(titleBar.Render(" Open project "))
	b.WriteString("\n\n")
	b.WriteString(m.pickerInput.View())
	b.WriteString("\n\n")
	if len(items) == 0 {
		b.WriteString(hintStyle.Render("type a project path, or open one to build a recent list"))
	} else {
		for i := 0; i < maxRows; i++ {
			label := truncate(items[i], inner-2)
			if i == m.pickerCursor {
				b.WriteString(selectedRow.Width(inner).Render("▸ " + label))
			} else {
				b.WriteString("  " + label)
			}
			if i < maxRows-1 {
				b.WriteString("\n")
			}
		}
		if len(items) > maxRows {
			b.WriteString("\n" + hintStyle.Render("  +"+itoa(len(items)-maxRows)+" more"))
		}
	}
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render(",o · type a path · ↑↓ pick recent · enter open · esc close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39")).
		Padding(1, 2).
		Width(w).
		Render(b.String())
}

// backlinkFooterLines renders the "↩ Linked mentions" panel under the editor for
// the open note (Obsidian-style). Empty when toggled off via :backlinks, or when
// nothing links here.
func (m VaultModel) backlinkFooterLines(w int) []string {
	if !m.showBacklinks || len(m.backlinks) == 0 {
		return nil
	}
	avail := max(8, w-2)
	lines := []string{backlinkHeaderStyle.Render(truncate("↩ Linked mentions ("+itoa(len(m.backlinks))+")", avail))}
	const maxShown = 5
	for i, n := range m.backlinks {
		if i >= maxShown {
			lines = append(lines, hintStyle.Render(truncate("  … +"+itoa(len(m.backlinks)-maxShown)+" more", avail)))
			break
		}
		title := n.Title
		if title == "" {
			title = n.Path
		}
		lines = append(lines, truncate("  • "+title, avail))
	}
	return lines
}

func (m VaultModel) titleView() string {
	t := "d-code"
	if name := m.svc.ProjectName(); name != "" {
		t += " · " + name
	}
	if m.svc.Offline() {
		t += "  (offline)"
	}
	return titleBar.Width(m.width).Render(t)
}

func (m VaultModel) statusView() string {
	if m.cmdMode {
		line := m.cmdLine.View()
		if h := m.cmdComp.Hint(); h != "" {
			line += "   " + hintStyle.Render(h)
		}
		// MaxWidth keeps a long wildmenu to the single status row.
		return statusBar.Width(m.width).MaxWidth(m.width).Render(line)
	}
	// Node-operation states render persistently (a flash would fade mid-decision).
	if len(m.confirmDel) > 0 {
		what := m.confirmDel[0]
		if len(m.confirmDel) > 1 {
			what = itoa(len(m.confirmDel)) + " items"
		}
		line := errStyle.Render("delete "+what+"?") + " " + hintStyle.Render("y confirm · any other key cancels")
		return statusBar.Width(m.width).MaxWidth(m.width).Render(line)
	}
	if m.pendingNode {
		line := noticeStyle.Render("node:") + " " + hintStyle.Render("(a)dd · (m)ove/rename · (d)elete · any other key cancels")
		return statusBar.Width(m.width).Render(line)
	}
	left := "[" + m.focusName() + "]"
	if m.pending > 0 {
		left += " " + m.spin.View() + " " + m.loadKind
	} else if m.err != nil {
		left += " " + errStyle.Render("error: "+m.err.Error())
	}
	if m.notice != "" && time.Since(m.noticeAt) < noticeTTL {
		return statusBar.Width(m.width).Render(left + "   " + noticeStyle.Render(m.notice))
	}
	hints := "⌃w h·l focus · : cmds · :learn <topic> · enter open · ⌃s save · ⌃c quit"
	switch {
	case m.pendingEdit != "":
		hints = noticeStyle.Render("proposed edit") + " · :apply to use it · :discard to drop it"
	case m.pendingWindow:
		hints = errStyle.Render("⌃w") + " window: h/l choose pane"
	case m.focus == paneSidebar:
		hints = "j/k move · enter open · ,o project · ,ff find · space mark · m node ops · : cmds"
	case m.focus == paneEditor && m.readOnly:
		hints = ":explain decode file · select+,d decode lines · :note save · ,ff find · ,n fold"
	case m.focus == paneEditor:
		hints = ":explain · :polish/:edit AI edit · select+:ask discuss · ,ff find · ⌃s save"
	case m.focus == paneChat:
		hints = "enter send · drag to copy · ⌥o/:copy copy reply · ⌃f/⌃b scroll"
	}
	return statusBar.Width(m.width).Render(left + "   " + hintStyle.Render(hints))
}

func (m VaultModel) focusName() string {
	switch m.focus {
	case paneSidebar:
		return "notes"
	case paneEditor:
		return "editor"
	case paneChat:
		return "chat"
	}
	return ""
}

// vChatTurn builds a one-element tutor history slice (test/readability helper).
func vChatTurn(role, content string) []tutor.ChatTurn {
	return []tutor.ChatTurn{{Role: role, Content: content}}
}

// itoa is a tiny non-negative int formatter (avoids pulling strconv in here).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// --- async commands & messages ---

type (
	vNotesMsg struct {
		notes     []core.NoteMeta
		tree      []core.TreeEntry
		truncated bool // the project exceeded the tree cap; the listing is partial
	}
	vOpenedMsg    struct{ note core.Note }
	vBacklinksMsg struct {
		path  string
		links []core.NoteMeta
	}
	vDeletedMsg          struct{ paths []string }
	vRenamedMsg          struct{ oldPath, newPath string }
	vMkdirMsg            struct{ path string }
	vGeneratedMsg        struct{ meta core.NoteMeta }
	vExplanationSavedMsg struct{ meta core.NoteMeta }
	vSavedMsg            struct{ meta core.NoteMeta }
	vErrMsg              struct {
		kind string
		err  error
	}
)

func vListCmd(svc *core.Service) tea.Cmd {
	return func() tea.Msg {
		notes, err := svc.ListNotes()
		if err != nil {
			return vErrMsg{kind: "list", err: err}
		}
		tree, truncated, err := svc.Tree()
		if err != nil {
			return vErrMsg{kind: "list", err: err}
		}
		return vNotesMsg{notes: notes, tree: tree, truncated: truncated}
	}
}

// vDeleteCmd removes the given notes/directories (a space-marked batch or the
// cursor row).
func vDeleteCmd(svc *core.Service, paths []string) tea.Cmd {
	return func() tea.Msg {
		for _, p := range paths {
			if err := svc.Delete(p); err != nil {
				return vErrMsg{kind: "delete", err: err}
			}
		}
		return vDeletedMsg{paths: paths}
	}
}

func vRenameCmd(svc *core.Service, oldPath, newPath string) tea.Cmd {
	return func() tea.Msg {
		if err := svc.Rename(oldPath, newPath); err != nil {
			return vErrMsg{kind: "move", err: err}
		}
		return vRenamedMsg{oldPath: oldPath, newPath: newPath}
	}
}

func vMkdirCmd(svc *core.Service, path string) tea.Cmd {
	return func() tea.Msg {
		if err := svc.MakeDir(path); err != nil {
			return vErrMsg{kind: "mkdir", err: err}
		}
		return vMkdirMsg{path: path}
	}
}

func vOpenCmd(svc *core.Service, path string) tea.Cmd {
	return func() tea.Msg {
		n, err := svc.OpenNote(path)
		if err != nil {
			return vErrMsg{kind: "open", err: err}
		}
		return vOpenedMsg{note: n}
	}
}

// vBacklinksCmd fetches the notes that link to path. Backlinks are advisory, so
// an error just yields an empty panel rather than a visible failure.
func vBacklinksCmd(svc *core.Service, path string) tea.Cmd {
	return func() tea.Msg {
		links, err := svc.Backlinks(path)
		if err != nil {
			return vBacklinksMsg{path: path, links: nil}
		}
		return vBacklinksMsg{path: path, links: links}
	}
}

func vGenCmd(svc *core.Service, request string) tea.Cmd {
	return func() tea.Msg {
		meta, err := svc.GenerateLesson(context.Background(), request)
		if err != nil {
			return vErrMsg{kind: "generate", err: err}
		}
		return vGeneratedMsg{meta: meta}
	}
}

func vSaveExplanationCmd(svc *core.Service, srcPath, explanation string) tea.Cmd {
	return func() tea.Msg {
		meta, err := svc.SaveExplanation(srcPath, explanation)
		if err != nil {
			return vErrMsg{kind: "save explanation", err: err}
		}
		return vExplanationSavedMsg{meta: meta}
	}
}

func vSaveCmd(svc *core.Service, path, body string) tea.Cmd {
	return func() tea.Msg {
		meta, err := svc.SaveNote(path, body)
		if err != nil {
			return vErrMsg{kind: "save", err: err}
		}
		return vSavedMsg{meta: meta}
	}
}

// vSaveOpenCmd saves a new note then opens it.
func vSaveOpenCmd(svc *core.Service, path, body string) tea.Cmd {
	return func() tea.Msg {
		if _, err := svc.SaveNote(path, body); err != nil {
			return vErrMsg{kind: "save", err: err}
		}
		n, err := svc.OpenNote(path)
		if err != nil {
			return vErrMsg{kind: "open", err: err}
		}
		return vOpenedMsg{note: n}
	}
}

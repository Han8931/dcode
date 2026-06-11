package editor

// CmdHistory adds readline-style ↑/↓ recall to a single-line command prompt
// (the editor's ":" and "/" lines, and the TUIs' global command lines).
type CmdHistory struct {
	items []string
	pos   int    // == len(items) means "live" (composing)
	draft string // stashed live input while navigating
}

// Open resets navigation for a freshly opened prompt.
func (h *CmdHistory) Open() {
	h.pos = len(h.items)
	h.draft = ""
}

// Record stores an executed command (consecutive duplicates collapse).
func (h *CmdHistory) Record(s string) {
	if s == "" {
		return
	}
	if n := len(h.items); n == 0 || h.items[n-1] != s {
		h.items = append(h.items, s)
	}
	h.pos = len(h.items)
	h.draft = ""
}

// Prev steps back through history; cur is the current input, stashed as the
// draft when navigation starts. ok is false at the oldest entry.
func (h *CmdHistory) Prev(cur string) (string, bool) {
	if len(h.items) == 0 || h.pos == 0 {
		return "", false
	}
	if h.pos == len(h.items) {
		h.draft = cur
	}
	h.pos--
	return h.items[h.pos], true
}

// Next steps forward, returning to the stashed draft past the newest entry.
func (h *CmdHistory) Next() (string, bool) {
	if h.pos >= len(h.items) {
		return "", false
	}
	h.pos++
	if h.pos == len(h.items) {
		return h.draft, true
	}
	return h.items[h.pos], true
}

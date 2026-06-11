package editor

import "strings"

// CmdCompleter adds vim-wildmenu-style Tab completion to a single-line command
// prompt (the TUIs' global ":" lines and the editor's own). The first Tab fills
// in the first command matching the typed prefix, repeated Tabs cycle forward
// (Shift-Tab backward), and stepping past the last match restores what was
// typed. Any other key ends the cycle — the caller Resets.
type CmdCompleter struct {
	prefix  string   // the live input when the cycle started
	matches []string // commands matching prefix, in the order given
	pos     int      // index on display; len(matches) means "back on the prefix"
}

// Next steps the cycle by dir (+1 for Tab, -1 for Shift-Tab) over the commands
// in names that start with the typed input, returning the text to show. The
// first call snapshots typed as the prefix; later calls keep matching against
// that snapshot, not the completed text. ok is false when nothing matches.
func (c *CmdCompleter) Next(typed string, names []string, dir int) (string, bool) {
	if c.matches == nil {
		c.prefix = typed
		c.matches = make([]string, 0, len(names))
		for _, n := range names {
			if n != typed && strings.HasPrefix(n, typed) {
				c.matches = append(c.matches, n)
			}
		}
		c.pos = len(c.matches) // the cycle starts from the bare prefix
	}
	n := len(c.matches)
	if n == 0 {
		return "", false
	}
	c.pos = ((c.pos+dir)%(n+1) + n + 1) % (n + 1)
	if c.pos == n {
		return c.prefix, true
	}
	return c.matches[c.pos], true
}

// Reset ends the cycle; the next Tab re-derives matches from the live input.
func (c *CmdCompleter) Reset() {
	*c = CmdCompleter{}
}

// Hint renders the cycle's candidates for a status line, bracketing the one on
// display: "clear  [compact]  copy". Empty unless a multi-match cycle is
// active — a single match needs no menu, the completed text says it all.
func (c *CmdCompleter) Hint() string {
	if len(c.matches) < 2 {
		return ""
	}
	parts := make([]string, len(c.matches))
	for i, name := range c.matches {
		if i == c.pos {
			name = "[" + name + "]"
		}
		parts[i] = name
	}
	return strings.Join(parts, "  ")
}

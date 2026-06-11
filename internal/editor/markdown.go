package editor

// markdown.go highlights markdown notes in the vault editor. Like the code
// highlighter it post-processes the textarea's rendered view, so every helper
// is ANSI-aware: escape sequences pass through untouched and the ambient SGR
// state (most notably the cursor-line background) is re-asserted after each
// styled chunk — see updateAmbient in editor.go.
//
// Styled structures: # headings, ```fenced code blocks``` (with full go/python
// rules when the fence names the language), `inline code`, [[wikilinks]],
// [text](url) links, > blockquotes, -/*/+ and 1./1) list markers, --- rules,
// and *italic*/**bold**/***both*** spans (star-flanked only — _underscores_
// are too common in technical notes to color reliably).
//
// Block state (an open fence or quote) is computed from the FULL buffer
// (mdScanBuffer) and re-synced to each rendered row via its line-number
// gutter, so scrolling can never change a row's styling: a fence whose ```
// opener sits above the viewport still renders as code. Without line numbers
// the state falls back to running top-to-bottom across the visible rows.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	mdHeadingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	mdCodeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("222"))
	mdLinkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("79"))
	// mdURLStyle dims the "(url)" target of a [text](url) link so the link text
	// carries the color and the noisy URL recedes.
	mdURLStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	mdBulletStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	mdRuleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	mdItalicStyle = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("216"))
	// Bold spans get a bright foreground on top of the attribute: many
	// terminals render the bold attribute alone almost invisibly, and chat
	// bodies sit on a dimmer neutral (252) that emphasis should pop out of.
	mdBoldStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("222"))
	mdBoldItalic  = lipgloss.NewStyle().Bold(true).Italic(true).Foreground(lipgloss.Color("219"))
	mdStrongStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
)

// lineState is the block context at the START of a buffer line: whether it
// sits inside a fenced code block (and that fence's language) or a blockquote.
type lineState struct {
	inFence   bool
	fenceLang string
	inQuote   bool
}

// mdState carries block context from row to row while rendering.
type mdState struct {
	gutter bool        // rows start with the textarea's "  12 " line numbers
	lines  []lineState // per-buffer-line state from mdScanBuffer (may be nil)

	cur     lineState // running state for the row being rendered
	inBlock bool      // block-comment state inside a code fence
}

// fenceRules maps a fence's language tag to real code-highlighting rules.
func fenceRules(lang string) (syntaxRules, bool) {
	switch lang {
	case "go", "golang":
		return goSyntax(), true
	case "python", "py":
		return pythonSyntax(), true
	}
	return syntaxRules{}, false
}

// classify advances st across one buffer line's content (gutter-free, left-
// trimmed). It is the single definition of the block grammar, shared by the
// buffer scan and the row renderer so they can never disagree.
func classify(content string, st *lineState) {
	switch {
	case strings.HasPrefix(content, "```"):
		if st.inFence {
			*st = lineState{}
		} else {
			st.inQuote = false
			st.inFence = true
			st.fenceLang = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(content, "```")))
		}
	case st.inFence:
		// fenced rows change nothing
	case content == "":
		st.inQuote = false // a blank line ends a blockquote
	case isMDHeading(content), isMDBullet(content), mdNumberedLen(content) > 0, isMDRule(content):
		st.inQuote = false // headings, list items, and rules interrupt one
	case strings.HasPrefix(content, ">"):
		st.inQuote = true
	}
	// any other line inside a quote is a lazy continuation: inQuote persists
}

func isMDHeading(content string) bool {
	n := strings.IndexFunc(content, func(r rune) bool { return r != '#' })
	return strings.HasPrefix(content, "#") && (n < 0 || (n <= 6 && content[n] == ' '))
}

func isMDBullet(content string) bool {
	return len(content) >= 2 &&
		(content[0] == '-' || content[0] == '*' || content[0] == '+') && content[1] == ' '
}

// mdNumberedLen reports an ordered-list marker opening the line — "1. " or
// "1) " — returning the marker's length ("12." → 3), or 0 when there is none.
// Like isMDBullet, the space after the marker is required, so "3.14 is pi"
// stays prose.
func mdNumberedLen(content string) int {
	j := 0
	for j < len(content) && content[j] >= '0' && content[j] <= '9' {
		j++
	}
	if j == 0 || j > 3 { // >999 is a paragraph that starts with a number, not a list
		return 0
	}
	if j+1 < len(content) && (content[j] == '.' || content[j] == ')') && content[j+1] == ' ' {
		return j + 1
	}
	return 0
}

// isMDRule matches a thematic break: a line of three or more of the same
// rule character (---, ***, ___) and nothing else.
func isMDRule(content string) bool {
	if len(content) < 3 {
		return false
	}
	c := content[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 1; i < len(content); i++ {
		if content[i] != c {
			return false
		}
	}
	return true
}

// mdScanBuffer computes the block state at the start of every buffer line.
// Rendering re-syncs to it via each row's line number, so what a row looks
// like can never depend on where the viewport happens to be scrolled.
func mdScanBuffer(value string) []lineState {
	lines := strings.Split(value, "\n")
	states := make([]lineState, len(lines))
	var st lineState
	for i, line := range lines {
		states[i] = st
		classify(strings.TrimLeft(line, " \t"), &st)
	}
	return states
}

func highlightMarkdown(s string, gutter bool, lines []lineState) string {
	rows := strings.SplitAfter(s, "\n")
	var b strings.Builder
	st := mdState{gutter: gutter, lines: lines}
	for _, row := range rows {
		// Strip the newline before styling: lipgloss pads multi-line renders
		// to block width, which would inject phantom spaces into the next row.
		nl := ""
		if strings.HasSuffix(row, "\n") {
			nl, row = "\n", row[:len(row)-1]
		}
		b.WriteString(highlightMarkdownRow(row, &st))
		b.WriteString(nl)
	}
	return b.String()
}

func highlightMarkdownRow(row string, st *mdState) string {
	// Re-sync the running state to the buffer scan whenever the row carries a
	// line number (soft-wrap continuations have a blank gutter and keep the
	// running state).
	if n, ok := rowLineNumber(row, st.gutter); ok && st.lines != nil && n-1 < len(st.lines) {
		st.cur = st.lines[n-1]
	}
	content := strings.TrimLeft(mdRowContent(row, st.gutter), " \t")
	before := st.cur
	classify(content, &st.cur)
	if !before.inFence || !st.cur.inFence {
		st.inBlock = false // entering/leaving a fence resets comment state
	}

	switch {
	case strings.HasPrefix(content, "```"): // the delimiter row itself
		return styleRowText(row, mdCodeStyle, st.gutter)
	case before.inFence:
		if rules, ok := fenceRules(before.fenceLang); ok {
			return highlightLine(row, rules, &st.inBlock)
		}
		return styleRowText(row, mdCodeStyle, st.gutter)
	case content == "":
		return row
	case isMDHeading(content):
		return styleRowText(row, mdHeadingStyle, st.gutter)
	case isMDRule(content):
		return styleRowText(row, mdRuleStyle, st.gutter)
	case isMDBullet(content):
		// Tint the marker, then give the item text the inline treatment.
		return mdMarkerRow(row, st.gutter, 1)
	case mdNumberedLen(content) > 0:
		return mdMarkerRow(row, st.gutter, mdNumberedLen(content))
	case strings.HasPrefix(content, ">") || before.inQuote:
		// "> …" opens a quote; soft-wrapped rows of a long quoted line and
		// lazy paragraph continuations stay quoted.
		return styleRowText(row, commentStyle, st.gutter)
	}
	return mdInline(row)
}

// mdInline styles the inline spans — `code`, [[wikilinks]], ***emphasis*** —
// wherever they appear in the row.
func mdInline(row string) string {
	ambient := ""
	return mdInlineSeg(row, &ambient)
}

func mdInlineSeg(row string, ambient *string) string {
	var b strings.Builder
	for i := 0; i < len(row); {
		if e := ansiEscapeEnd(row, i); e > i {
			updateAmbient(row[i:e], ambient)
			b.WriteString(row[i:e])
			i = e
			continue
		}
		if row[i] == '`' {
			if _, after, ok := textIndex(row, i+1, "`"); ok {
				b.WriteString(renderToken(row[i:after], mdCodeStyle, ambient))
				i = after
				continue
			}
		}
		if afterOpen, ok := textMatch(row, i, "[["); ok {
			if _, after, ok := textIndex(row, afterOpen, "]]"); ok {
				b.WriteString(renderToken(row[i:after], mdLinkStyle, ambient))
				i = after
				continue
			}
		}
		// [text](url): the link text carries the color, the url is dimmed.
		if row[i] == '[' {
			if at, afterBracket, ok := textIndex(row, i+1, "]("); ok {
				if _, afterURL, ok := textIndex(row, afterBracket, ")"); ok {
					b.WriteString(renderToken(row[i:at+1], mdLinkStyle, ambient))
					b.WriteString(renderToken(row[at+1:afterURL], mdURLStyle, ambient))
					i = afterURL
					continue
				}
			}
		}
		if row[i] == '*' {
			if span, style, next := mdEmphasis(row, i); next > i {
				b.WriteString(renderToken(span, style, ambient))
				i = next
				continue
			}
		}
		b.WriteByte(row[i])
		i++
	}
	return b.String()
}

// mdEmphasis matches an emphasis span opening at text position i: *italic*,
// **bold**, ***both***, or ****strong****. The opener must touch text (no space after it) and
// the closer must touch text before it, CommonMark-flanking style, so a stray
// "2 * 3" never styles half the line. All matching is escape-aware: the
// cursor sitting on a delimiter splits it with SGR sequences, and the span
// must not flicker away while it does. Returns the span (delimiters
// included), its style, and the offset just past it; next == i means no match.
func mdEmphasis(row string, i int) (span string, style lipgloss.Style, next int) {
	n, j := 0, i
	for n < 4 {
		after, ok := textMatch(row, j, "*")
		if !ok {
			break
		}
		j, n = after, n+1
	}
	if c, ok := nextTextByte(row, j); !ok || c == ' ' || c == '*' {
		return "", style, i
	}
	delim := strings.Repeat("*", n)
	at, after, ok := textIndex(row, j, delim)
	for ok {
		if c, cok := lastTextByte(row, j, at); cok && c != ' ' {
			break // flanking holds: this is the closer
		}
		at, after, ok = textIndex(row, after, delim)
	}
	if !ok {
		return "", style, i
	}
	switch n {
	case 3:
		style = mdBoldItalic
	case 4:
		style = mdStrongStyle
	case 2:
		style = mdBoldStyle
	default:
		style = mdItalicStyle
	}
	return row[i:after], style, after
}

// mdMarkerRow tints a list marker of markerLen text bytes ("-" is 1, "12." is
// 3) under any indentation, leaving the item text to the inline pass. The
// marker scan is escape-aware: the cursor sitting on a marker digit splits it
// with SGR sequences.
func mdMarkerRow(row string, gutter bool, markerLen int) string {
	i := gutterEnd(row, gutter)
	for i < len(row) { // step over the item's indentation
		if e := ansiEscapeEnd(row, i); e > i {
			i = e
			continue
		}
		if row[i] != ' ' {
			break
		}
		i++
	}
	if i >= len(row) {
		return row
	}
	j, seen := i, 0
	for j < len(row) && seen < markerLen { // span markerLen text bytes
		if e := ansiEscapeEnd(row, j); e > j {
			j = e
			continue
		}
		j++
		seen++
	}
	ambient := ""
	updateAmbient(row[:i], &ambient)
	var b strings.Builder
	b.WriteString(row[:i])
	b.WriteString(renderToken(row[i:j], mdBulletStyle, &ambient))
	b.WriteString(mdInlineSeg(row[j:], &ambient))
	return b.String()
}

// styleRowText styles everything after the line-number gutter with style,
// leaving the gutter and embedded escape sequences intact.
func styleRowText(row string, style lipgloss.Style, gutter bool) string {
	split := gutterEnd(row, gutter)
	ambient := ""
	updateAmbient(row[:split], &ambient)
	return row[:split] + renderToken(row[split:], style, &ambient)
}

// mdRowContent returns the row's visible text — with the "  12 " line-number
// gutter stripped when the textarea draws one — for classifying the row's
// markdown structure. The strip is gated on gutter so a note line that really
// starts with digits ("2 * 3 = 6") is never misread as gutter + bullet.
func mdRowContent(row string, gutter bool) string {
	vis := stripANSI(row)
	vis = strings.TrimRight(vis, "\n")
	t := strings.TrimLeft(vis, " ")
	if !gutter {
		return t
	}
	j := 0
	for j < len(t) && t[j] >= '0' && t[j] <= '9' {
		j++
	}
	if j > 0 && j < len(t) && t[j] == ' ' {
		t = t[j+1:]
	}
	return t
}

// gutterEnd returns the byte offset just past the line-number gutter (leading
// spaces, digits, and one following space), skipping escape sequences. When
// the textarea draws no gutter, only leading spaces are skipped.
func gutterEnd(row string, gutter bool) int {
	i := 0
	skip := func(match func(byte) bool) {
		for i < len(row) {
			if e := ansiEscapeEnd(row, i); e > i {
				i = e
				continue
			}
			if !match(row[i]) {
				return
			}
			i++
		}
	}
	skip(func(c byte) bool { return c == ' ' })
	if !gutter {
		return i
	}
	start := i
	skip(func(c byte) bool { return c >= '0' && c <= '9' })
	if i > start { // had a line number: consume the single space after it
		for e := ansiEscapeEnd(row, i); e > i; e = ansiEscapeEnd(row, i) {
			i = e
		}
		if i < len(row) && row[i] == ' ' {
			i++
		}
	}
	return i
}

// textMatch reports whether sub occurs at text position i — skipping any
// escape sequences interleaved between its bytes (the cursor's styling can
// split a "**" or "[[" delimiter) — and returns the offset just past it.
func textMatch(s string, i int, sub string) (after int, ok bool) {
	j, k := i, 0
	for k < len(sub) {
		if e := ansiEscapeEnd(s, j); e > j {
			j = e
			continue
		}
		if j >= len(s) || s[j] != sub[k] {
			return i, false
		}
		j++
		k++
	}
	return j, true
}

// textIndex finds the next text-level occurrence of sub at or after from,
// returning its byte offset and the offset just past it.
func textIndex(s string, from int, sub string) (at, after int, ok bool) {
	for i := from; i < len(s); {
		if e := ansiEscapeEnd(s, i); e > i {
			i = e
			continue
		}
		if after, ok := textMatch(s, i, sub); ok {
			return i, after, true
		}
		i++
	}
	return 0, 0, false
}

// nextTextByte returns the first text byte at or after i.
func nextTextByte(s string, i int) (byte, bool) {
	for i < len(s) {
		if e := ansiEscapeEnd(s, i); e > i {
			i = e
			continue
		}
		return s[i], true
	}
	return 0, false
}

// lastTextByte returns the last text byte in s[from:to].
func lastTextByte(s string, from, to int) (b byte, ok bool) {
	for i := from; i < to; {
		if e := ansiEscapeEnd(s, i); e > i {
			i = e
			continue
		}
		b, ok = s[i], true
		i++
	}
	return b, ok
}

// rowLineNumber parses the row's "  12 " gutter into its buffer line number.
func rowLineNumber(row string, gutter bool) (int, bool) {
	if !gutter {
		return 0, false
	}
	t := strings.TrimLeft(stripANSI(row), " ")
	j := 0
	for j < len(t) && t[j] >= '0' && t[j] <= '9' {
		j++
	}
	if j == 0 || j >= len(t) || t[j] != ' ' {
		return 0, false
	}
	n := 0
	for _, c := range t[:j] {
		n = n*10 + int(c-'0')
	}
	return n, true
}

// stripANSI removes escape sequences, leaving the visible text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if e := ansiEscapeEnd(s, i); e > i {
			i = e
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

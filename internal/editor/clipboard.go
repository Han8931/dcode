package editor

import (
	"os"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"
)

// copyToSystem mirrors yanked text to the system clipboard two ways at once:
// the native clipboard (pbcopy / xclip / wl-copy via atotto) for local
// sessions, and an OSC 52 escape written to the terminal so copying also
// works over SSH. A package variable so tests can stub it. Errors are
// ignored — the in-app register works regardless.
var copyToSystem = func(text string) {
	_, _ = osc52.New(text).WriteTo(os.Stderr)
	_ = clipboard.WriteAll(text)
}

// pasteFromSystem reads the system clipboard (native only — terminals don't
// allow OSC 52 reads for security). A package variable so tests can stub it.
var pasteFromSystem = func() (string, error) {
	return clipboard.ReadAll()
}

// setYank stores yanked text in the unnamed register and mirrors it to the
// system clipboard, so yy / visual-y can be pasted into other apps. Deletes
// (x, dd, c…) stay register-only: silently clobbering the system clipboard
// on every delete would be hostile.
func (m *Model) setYank(text string, linewise bool) {
	m.register, m.regLinewise = text, linewise
	copyToSystem(text)
}

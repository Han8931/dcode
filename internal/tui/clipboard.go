package tui

import (
	"os"
	"path/filepath"
	"time"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"

	"dcode/internal/vault"
)

// copyToClipboard puts text on the system clipboard two ways at once: the
// native clipboard (pbcopy / xclip / wl-copy via atotto) for local sessions,
// and an OSC 52 escape written to the terminal so copying also works over SSH
// in terminals that support it. It is a package variable so tests can stub it
// out instead of touching the real clipboard.
//
// The returned error reflects only the native path — OSC 52 is fire-and-forget
// (a terminal that doesn't support it silently ignores the sequence).
var copyToClipboard = func(text string) error {
	_, _ = osc52.New(text).WriteTo(os.Stderr)
	return clipboard.WriteAll(text)
}

// pasteFromClipboard reads the system clipboard (native only — terminals don't
// allow OSC 52 reads for security). A package variable so tests can stub it.
var pasteFromClipboard = func() (string, error) {
	return clipboard.ReadAll()
}

// exportChat writes the chat transcript to dir/chat-<label>-<timestamp>.md
// (":export" in both TUIs) and returns the status-bar message.
func exportChat(c *chatModel, dir, label string) string {
	text, ok := c.transcript()
	if !ok {
		return "nothing to export yet — ask the tutor something first"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "⚠ export failed: " + err.Error()
	}
	if label = vault.Slug(label); label == "untitled" {
		label = "chat"
	}
	path := filepath.Join(dir, "chat-"+label+"-"+time.Now().Format("20060102-150405")+".md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "⚠ export failed: " + err.Error()
	}
	return "exported chat to " + path
}

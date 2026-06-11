package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// Async streaming infrastructure. A blocking tutor chat call is wrapped in a
// tea.Cmd that runs on its own goroutine and delivers fragments back to the
// Update loop, so the UI never blocks on the network.

// streamChunkMsg carries one streamed chat fragment (or its terminal state)
// from the worker goroutine into the Update loop.
type streamChunkMsg struct {
	delta string // one text fragment ("" on the final message)
	full  string // the assembled reply, set when done
	done  bool
	err   error
}

// startChatStream runs fn (a streaming chat call) on a goroutine, forwarding
// each delta through the returned channel, then a final done/err message. The
// returned cmd delivers the first message; the Update loop re-arms with
// listenStream until done.
func startChatStream(ctx context.Context, fn func(context.Context, func(string)) (string, error)) (chan streamChunkMsg, tea.Cmd) {
	ch := make(chan streamChunkMsg, 64)
	go func() {
		full, err := fn(ctx, func(d string) { ch <- streamChunkMsg{delta: d} })
		ch <- streamChunkMsg{done: true, full: full, err: err}
	}()
	return ch, listenStream(ch)
}

func listenStream(ch chan streamChunkMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

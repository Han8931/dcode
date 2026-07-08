package core

// verify.go is the :verify flow — the evidence rung of dcode's verification
// ladder (:review = opinion, :tests = checkable claim, :verify = evidence). It
// runs the project's test suite (auto-detected, or a command the user names),
// streams the live output into the chat, and — when the run fails and an AI
// provider is configured — follows with an interpretation of the failures
// grounded in the output and the current diff.
//
// This is the ONE place dcode executes project code, and it only ever happens
// after the explicit :verify! confirmation in the UI (a bare :verify shows the
// command without running it). The run itself needs no AI at all, so :verify
// works fully offline; only the interpretation is model-backed.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dcode/internal/tutor"
)

const verifySystemPrompt = `You are interpreting a failed test/build run for a developer.
From the command, its output, and (when given) the current diff, report:
- What failed: each distinct failure in plain language, with file:line when the
  output shows one.
- Why: the likeliest cause, grounded in the output — and in the diff, if the
  failure looks caused by the change.
- Fix: the most direct next step for each failure.
Do NOT invent failures that are not in the output. Be brief. Markdown.`

const (
	verifyTimeout    = 10 * time.Minute // hard cap on a test run
	maxVerifyStream  = 24000            // run output streamed into the chat
	maxVerifyTail    = 8000             // output tail sent for interpretation
	maxVerifyDiffCtx = 8000             // current-diff context for interpretation
)

// DetectTestCommand guesses the project's test command from its root files.
// ok is false when no known project marker is found — the UI then asks the
// user to name a command (:verify <command>).
func (s *Service) DetectTestCommand() (cmd string, ok bool) {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(s.ProjectRoot(), name))
		return err == nil
	}
	switch {
	case has("go.mod"):
		return "go test ./...", true
	case has("Cargo.toml"):
		return "cargo test", true
	case has("package.json"):
		return "npm test", true
	case has("pytest.ini") || has("pyproject.toml") || has("setup.py"):
		return "pytest", true
	case has("Makefile"):
		return "make test", true
	}
	return "", false
}

// deltaWriter tees a process's combined output: each write is forwarded to
// onDelta (up to a streaming cap, so a huge run can't flood the chat) and the
// tail is retained for the model to interpret. Stdout and stderr share it, so
// writes are locked.
type deltaWriter struct {
	mu      sync.Mutex
	onDelta func(string)
	sent    int
	capped  bool
	tail    []byte
}

func (w *deltaWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.onDelta != nil {
		switch {
		case w.sent+len(p) <= maxVerifyStream:
			w.onDelta(string(p))
			w.sent += len(p)
		case !w.capped:
			w.onDelta("\n… (output truncated — full tail still analyzed)\n")
			w.capped = true
		}
	}
	w.tail = append(w.tail, p...)
	if len(w.tail) > maxVerifyTail {
		w.tail = w.tail[len(w.tail)-maxVerifyTail:]
	}
	return len(p), nil
}

// VerifyStream runs command (via the shell) in the project root, streaming its
// combined output into onDelta. A failing run is not an error — it is the
// result: when the exit is non-zero and an AI provider is configured, the
// failure tail (plus the working diff, if any) is interpreted by the model,
// also streamed. The returned text is what should be kept (verdict plus any
// interpretation). Errors are reserved for not being able to run at all.
func (s *Service) VerifyStream(ctx context.Context, command string, onDelta func(string)) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("no test command to run")
	}
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	if onDelta != nil {
		onDelta("$ " + command + "\n\n")
	}
	w := &deltaWriter{onDelta: onDelta}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = s.ProjectRoot()
	cmd.Stdout = w
	cmd.Stderr = w
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err() // cancelled (Esc) or timed out — not a verdict
	}

	if runErr == nil {
		verdict := "\n✓ PASSED — `" + command + "` exited 0."
		if onDelta != nil {
			onDelta(verdict)
		}
		return verdict, nil
	}
	if _, isExit := runErr.(*exec.ExitError); !isExit {
		// The command could not run at all (not found, not executable…).
		return "", fmt.Errorf("could not run %q: %v", command, runErr)
	}

	verdict := "\n✗ FAILED — `" + command + "`: " + runErr.Error() + "."
	if onDelta != nil {
		onDelta(verdict + "\n")
	}
	if s.Offline() {
		// The run itself is the value; interpretation just needs a provider.
		note := "\n(offline — configure an AI provider for a failure interpretation)"
		if onDelta != nil {
			onDelta(note)
		}
		return verdict + note, nil
	}

	// Interpret the failure, grounded in the output tail and the working diff.
	if onDelta != nil {
		onDelta("\n— interpreting the failure —\n\n")
	}
	var b strings.Builder
	b.WriteString("Command: " + command + "\n\nOutput tail:\n\n```\n" + string(w.tail) + "\n```")
	if diff, err := s.gitDiff(ctx, DiffRange("")); err == nil && strings.TrimSpace(diff) != "" {
		b.WriteString("\n\nCurrent working diff (the likeliest suspect):\n\n```diff\n" +
			clampChars(diff, maxVerifyDiffCtx) + "\n```")
	}
	hist := []tutor.ChatTurn{{Role: "user", Content: b.String()}}
	interp, err := s.tutor.StreamConversation(ctx, verifySystemPrompt, hist, onDelta)
	if err != nil {
		return verdict, nil // the run's verdict stands even if interpretation fails
	}
	return verdict + "\n\n" + interp, nil
}

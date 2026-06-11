package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dcode/internal/config"
	"dcode/internal/tutor"
	"dcode/internal/vault"
)

// polishServer fakes an OpenAI-compatible streaming endpoint that replies with
// the given content split into SSE chunks, capturing the request body.
func polishServer(t *testing.T, chunks []string, gotBody *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			payload, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": map[string]string{"content": c}}},
			})
			_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func polishService(t *testing.T, srvURL string) *Service {
	t.Helper()
	code, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	notes, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tut := tutor.New(config.AIConfig{Provider: "compatible", BaseURL: srvURL, Model: "m"})
	return New(code, notes, tut)
}

func TestPolishNoteStreamsAndSendsNoteAndInstruction(t *testing.T) {
	var body string
	srv := polishServer(t, []string{"# Title\n", "\npolished body"}, &body)
	defer srv.Close()
	svc := polishService(t, srv.URL)

	var deltas []string
	full, err := svc.PolishNote(context.Background(),
		"# title\n\nrough body", "make it formal",
		func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatal(err)
	}
	if full != "# Title\n\npolished body" {
		t.Fatalf("full = %q", full)
	}
	if len(deltas) != 2 {
		t.Fatalf("expected 2 streamed chunks, got %v", deltas)
	}
	// Both the instruction and the original note ride along in the request.
	if !strings.Contains(body, "make it formal") || !strings.Contains(body, "rough body") {
		t.Fatalf("request missing instruction or note:\n%s", body)
	}
}

func TestPolishNoteDefaultInstruction(t *testing.T) {
	var body string
	srv := polishServer(t, []string{"x"}, &body)
	defer srv.Close()
	svc := polishService(t, srv.URL)

	if _, err := svc.PolishNote(context.Background(), "note", "", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, DefaultPolishInstruction) {
		t.Fatalf("empty instruction should fall back to the default:\n%s", body)
	}
}

func TestPolishNoteStripsWrappingFence(t *testing.T) {
	srv := polishServer(t, []string{"```markdown\n", "# Note\n\nbody\n", "```"}, new(string))
	defer srv.Close()
	svc := polishService(t, srv.URL)

	full, err := svc.PolishNote(context.Background(), "x", "y", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if full != "# Note\n\nbody" {
		t.Fatalf("wrapping fence not stripped: %q", full)
	}
}

func TestStripNoteFenceLeavesInnerFences(t *testing.T) {
	// A note that merely *contains* a code block must be left untouched.
	note := "Intro\n\n```go\nfmt.Println(1)\n```\n\nmore"
	if got := stripNoteFence(note); got != note {
		t.Fatalf("inner fence wrongly stripped:\n%q", got)
	}
}

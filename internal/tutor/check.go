package tutor

// check.go provides connection diagnostics for the `dcode check` command: a
// snapshot of how the tutor was configured, the provider's model list, and a
// tiny round-trip request — so "is my AI set up right?" has a one-command answer.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Info describes how the tutor resolved its configuration.
type Info struct {
	BaseURL string
	Model   string
	KeySet  bool
	Offline bool
}

// Info reports the resolved connection settings (the key itself is never exposed).
func (t *Tutor) Info() Info {
	return Info{BaseURL: t.baseURL, Model: t.model, KeySet: t.apiKey != "", Offline: t.offline}
}

// Models fetches the provider's model list (GET {base}/models), so the check
// command can confirm the configured model actually exists.
func (t *Tutor) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// Ping sends a minimal chat request and returns the round-trip time. It uses
// the same transport as real AI calls, so a passing ping means explanations,
// note edits, summaries, and chat will work.
func (t *Tutor) Ping(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	_, err := t.chat(ctx, "Reply with the single word: ok", "ping")
	return time.Since(start), err
}

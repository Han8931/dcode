// Package tutor talks to a language model for code explanations, note edits,
// project summaries, diff explanations, and grounded chat. Every provider is
// reached through the OpenAI-compatible chat-completions API (POST
// {base}/chat/completions), so OpenAI, Ollama, and other compatible gateways all
// work with the same code — only the base URL, model, and key differ (see
// config.AIConfig).
//
// If no API key is configured (and the provider isn't a local Ollama), the
// tutor runs offline and returns clear setup guidance instead of making model
// requests.
package tutor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"dcode/internal/config"
)

// Tutor issues model requests for one configured provider.
type Tutor struct {
	client  *http.Client
	baseURL string
	model   string
	apiKey  string
	offline bool
}

// openAIBaseURL is the one endpoint that genuinely requires an API key. Local
// servers (Ollama, LM Studio, llama.cpp) and many gateways accept unauthenticated
// requests, so a missing key must not force them offline.
const openAIBaseURL = "https://api.openai.com/v1"

// defaultTimeout bounds each model request when the config doesn't set one.
// Local models can take tens of seconds to load and to generate full replies.
const defaultTimeout = 120 * time.Second

// New builds a Tutor from config. The API key comes from the configured
// environment variable, falling back to a key pasted directly in the config
// (api_key). It runs offline only when a key is required (the official OpenAI
// endpoint) but none is set.
func New(cfg config.AIConfig) *Tutor {
	key := ""
	if cfg.APIKeyEnv != "" {
		key = os.Getenv(cfg.APIKeyEnv)
	}
	if key == "" {
		key = strings.TrimSpace(cfg.APIKey)
	}
	baseURL := strings.TrimRight(cfg.ResolveBaseURL(), "/")
	offline := key == "" && baseURL == openAIBaseURL

	timeout := defaultTimeout
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}

	return &Tutor{
		client:  &http.Client{Timeout: timeout},
		baseURL: baseURL,
		model:   cfg.Model,
		apiKey:  key,
		offline: offline,
	}
}

// Offline reports whether the tutor has no configured provider.
func (t *Tutor) Offline() bool { return t.offline }

// --- OpenAI-compatible chat-completions wire types ---

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (t *Tutor) chat(ctx context.Context, system, user string) (string, error) {
	return t.chatRaw(ctx, []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
}

// chatRaw posts a full message list to the chat-completions endpoint and
// returns the assistant's reply. It is the shared transport for the one-shot
// helpers (explain, polish, overview, diff) and the multi-turn Chat conversation.
func (t *Tutor) chatRaw(ctx context.Context, messages []chatMessage) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:    t.model,
		Messages: messages,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ai request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", err
	}
	if cr.Error != nil {
		return "", fmt.Errorf("ai error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("ai returned no choices")
	}
	return cr.Choices[0].Message.Content, nil
}

// ChatTurn is one message in the free-form assistant conversation. The caller
// owns and accumulates the history; Chat prepends its own system prompt.
type ChatTurn struct {
	Role    string // "user" | "assistant"
	Content string
}

const chatSystemPrompt = "You are an expert software engineer helping another developer " +
	"understand a codebase they are reading. Answer their questions directly and concretely, " +
	"grounded in the code and context provided, and reference the real identifiers from it. " +
	"Keep replies concise; include a short code snippet only when it aids understanding."

// Chat continues a free-form assistant conversation. history holds the prior
// user/assistant turns; Chat prepends the assistant system prompt and returns the
// assistant's next reply. Offline, it returns a canned message.
func (t *Tutor) Chat(ctx context.Context, history []ChatTurn) (string, error) {
	return t.ChatStream(ctx, "", history, nil)
}

// ChatStream continues the assistant conversation, streaming the reply.
// studyContext, when non-empty, is what the user is currently looking at
// (note body, selected code, or source file) and is injected as a system message so
// answers stay grounded in the current material. onDelta (optional) receives
// each text chunk as it arrives; the full reply is returned at the end.
func (t *Tutor) ChatStream(ctx context.Context, studyContext string, history []ChatTurn, onDelta func(string)) (string, error) {
	if t.offline {
		s := "I'm offline right now (no AI provider configured), so I can't answer " +
			"free-form questions. Try writing code and running the tests — you'll still " +
			"get feedback from the built-in content."
		if onDelta != nil {
			onDelta(s)
		}
		return s, nil
	}

	msgs := make([]chatMessage, 0, len(history)+2)
	msgs = append(msgs, chatMessage{Role: "system", Content: chatSystemPrompt})
	if studyContext != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: "Context — what the user is " +
			"currently reading. Ground your answers in this material and the conversation:\n\n" + studyContext})
	}
	for _, h := range history {
		msgs = append(msgs, chatMessage{Role: h.Role, Content: h.Content})
	}
	if onDelta == nil {
		return t.chatRaw(ctx, msgs)
	}
	return t.chatStreamRaw(ctx, msgs, onDelta)
}

// StreamConversation streams a reply over history with system as THE system
// prompt. Unlike ChatStream — which frames every exchange as assistant Q&A and
// demotes extra instructions to context a small model may ignore — this lets
// purpose-built flows (e.g. :explain, :polish) own the conversation's framing.
func (t *Tutor) StreamConversation(ctx context.Context, system string, history []ChatTurn, onDelta func(string)) (string, error) {
	if t.offline {
		s := "I'm offline right now (no AI provider configured)."
		if onDelta != nil {
			onDelta(s)
		}
		return s, nil
	}
	msgs := make([]chatMessage, 0, len(history)+1)
	msgs = append(msgs, chatMessage{Role: "system", Content: system})
	for _, h := range history {
		msgs = append(msgs, chatMessage{Role: h.Role, Content: h.Content})
	}
	if onDelta == nil {
		return t.chatRaw(ctx, msgs)
	}
	return t.chatStreamRaw(ctx, msgs, onDelta)
}

// chatStreamRaw posts a streaming chat-completions request (SSE) and feeds each
// content delta to onDelta, returning the assembled reply.
func (t *Tutor) chatStreamRaw(ctx context.Context, messages []chatMessage, onDelta func(string)) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:    t.model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ai request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // tolerate keep-alives / unknown event shapes
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			full.WriteString(chunk.Choices[0].Delta.Content)
			onDelta(chunk.Choices[0].Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil && full.Len() == 0 {
		return "", err
	}
	if full.Len() == 0 {
		return "", fmt.Errorf("ai returned no streamed content")
	}
	return full.String(), nil
}

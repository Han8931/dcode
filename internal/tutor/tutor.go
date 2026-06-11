// Package tutor talks to a language model to generate lessons, challenges, and
// feedback. Every provider is reached through the OpenAI-compatible
// chat-completions API (POST {base}/chat/completions), so OpenAI, Ollama, and
// other compatible gateways all work with the same code — only the base URL,
// model, and key differ (see config.AIConfig).
//
// If no API key is configured (and the provider isn't a local Ollama), the
// tutor falls back to a small built-in lesson + challenge so the app is fully
// runnable offline for trying out the editor loop.
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

// Challenge is a single exercise. The Tests are machine-checkable assertions
// (Python) that define correctness; the executor runs them against the
// learner's solution.
type Challenge struct {
	ID          string   `json:"id"`
	Prompt      string   `json:"prompt"`
	StarterCode string   `json:"starter_code"`
	Tests       []string `json:"tests"`
	// Lang is the programming language the tests run in ("python" or "go").
	// Empty means Python, for backward compatibility with LLM-generated content.
	Lang string `json:"-"`
}

// Tutor issues model requests for one configured provider.
type Tutor struct {
	client  *http.Client
	baseURL string
	model   string
	apiKey  string
	offline bool
	level   string // "beginner" | "intermediate" | "advanced" | "" (unset)
}

// SetLevel sets the learner's experience level, which is woven into the lesson,
// challenge, and feedback prompts to tune difficulty.
func (t *Tutor) SetLevel(level string) { t.level = level }

// levelClause returns a sentence steering content difficulty, or "" when unset.
func (t *Tutor) levelClause() string {
	switch t.level {
	case "beginner":
		return " The learner is a BEGINNER: keep it simple, explain jargon, and avoid advanced features."
	case "intermediate":
		return " The learner is at an INTERMEDIATE level: you can assume the basics are known."
	case "advanced":
		return " The learner is ADVANCED: use idiomatic, non-trivial examples and challenges."
	}
	return ""
}

// openAIBaseURL is the one endpoint that genuinely requires an API key. Local
// servers (Ollama, LM Studio, llama.cpp) and many gateways accept unauthenticated
// requests, so a missing key must not force them offline.
const openAIBaseURL = "https://api.openai.com/v1"

// defaultTimeout bounds each model request when the config doesn't set one.
// Local models can take tens of seconds to load and to generate a full lesson.
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

// Offline reports whether the tutor is using built-in content (no provider).
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
// helpers (Lesson/Challenge/Feedback) and the multi-turn Chat conversation.
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

// Lesson returns a short explanation of a topic with one worked example.
func (t *Tutor) Lesson(ctx context.Context, topic string) (string, error) {
	if t.offline {
		return offlineLesson(topic), nil
	}
	system := "You are a concise programming tutor. Explain the requested topic " +
		"in 4-8 sentences with exactly one short worked code example. Plain text, no markdown headers." +
		t.levelClause()
	return t.chat(ctx, system, "Teach me about: "+topic)
}

// Challenge returns a checkable exercise for a topic.
func (t *Tutor) Challenge(ctx context.Context, topic string) (Challenge, error) {
	if t.offline {
		return offlineChallenge(topic), nil
	}

	system := `You are a programming tutor that creates one coding exercise.
Respond with ONLY a JSON object, no prose, no markdown fences, matching:
{
  "id": "short-kebab-id",
  "prompt": "what the learner must implement, including the function name/signature",
  "starter_code": "a Python stub the learner edits, e.g. a def with pass",
  "tests": ["assert ...", "assert ..."]
}
The tests must be standalone Python assert statements that import nothing and
exercise the function the learner writes. Provide 3-5 tests.` + t.levelClause()

	raw, err := t.chat(ctx, system, "Create an exercise about: "+topic)
	if err != nil {
		return Challenge{}, err
	}

	ch, err := parseChallenge(raw)
	if err != nil {
		return Challenge{}, fmt.Errorf("could not parse challenge: %w", err)
	}
	if ch.ID == "" {
		ch.ID = slug(topic)
	}
	return ch, nil
}

// Feedback returns a hint (on failure) or encouragement (on pass). It never
// reveals the full solution.
func (t *Tutor) Feedback(ctx context.Context, ch Challenge, code, runOutput string, passed bool) (string, error) {
	if t.offline {
		return offlineFeedback(passed), nil
	}

	system := "You are a supportive programming tutor giving feedback on a learner's code. " +
		"If their solution passed, congratulate briefly and note one thing they did well. " +
		"If it failed, give detailed but concise diagnostic feedback: identify the likely bug, " +
		"quote or name the relevant line/function when possible, explain why the observed output fails the test, " +
		"and give the next small debugging step. " +
		"NEVER provide the full corrected solution or write the function for them. Keep it to 1 short paragraph or 3 bullets."

	status := "PASSED"
	if !passed {
		status = "FAILED"
	}
	user := fmt.Sprintf("Challenge: %s\n\nLearner's code:\n%s\n\nTest result: %s\nProgram output:\n%s",
		ch.Prompt, code, status, runOutput)

	return t.chat(ctx, system, user)
}

// ChatTurn is one message in the free-form tutoring conversation. The caller
// owns and accumulates the history; Chat prepends its own system prompt.
type ChatTurn struct {
	Role    string // "user" | "assistant"
	Content string
}

const chatSystemPrompt = "You are a supportive, concise programming tutor having a " +
	"conversation with a learner who is working through coding exercises. Answer their " +
	"questions and nudge them toward understanding. NEVER write the full solution for them. " +
	"Keep replies short — a few sentences, with at most a tiny illustrative snippet."

// Chat continues a free-form tutoring conversation. history holds the prior
// user/assistant turns; Chat prepends the tutor system prompt and returns the
// assistant's next reply. Offline, it returns a canned message.
func (t *Tutor) Chat(ctx context.Context, history []ChatTurn) (string, error) {
	return t.ChatStream(ctx, "", history, nil)
}

// ChatStream continues the tutoring conversation, streaming the reply.
// studyContext, when non-empty, is what the learner is currently looking at
// (note body, challenge, their code) and is injected as a system message so
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
		msgs = append(msgs, chatMessage{Role: "system", Content: "Context — what the learner is " +
			"currently studying. Ground your answers in this material and the conversation:\n\n" + studyContext})
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
// prompt. Unlike ChatStream — which frames every exchange as tutor Q&A and
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

// parseChallenge extracts a Challenge from a model response, tolerating
// accidental markdown fences around the JSON.
func parseChallenge(raw string) (Challenge, error) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return Challenge{}, fmt.Errorf("no JSON object found")
	}

	var ch Challenge
	if err := json.Unmarshal([]byte(s[start:end+1]), &ch); err != nil {
		return Challenge{}, err
	}
	return ch, nil
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

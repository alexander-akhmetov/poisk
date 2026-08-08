package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type Client struct {
	baseURL   string
	apiKey    string
	model     string
	extraBody map[string]any
	// autoExtraBody is added to requests when the model name says the model
	// reasons by default. Unlike extraBody it is dropped if the server rejects
	// it, so a name match cannot break a server that does not know the field.
	autoExtraBody map[string]any
	sendAuto      atomic.Bool
	http          *http.Client
}

type Option func(*Client)

// WithExtraBody adds fields to every chat completion request body. Reasoning
// models are the reason it exists: they spend the token budget on thinking and
// return empty content, and the switch that turns thinking off is
// server-specific (llama.cpp and vLLM take
// chat_template_kwargs.enable_thinking, others take reasoning_effort).
func WithExtraBody(extra map[string]any) Option {
	return func(c *Client) { c.extraBody = extra }
}

// qwen3Pattern matches the Qwen3 family in a model name, after any routing
// prefix ("mac-studio/qwen3.6-35b-a3b", "Qwen/Qwen3-32B", "qwen3:8b").
var qwen3Pattern = regexp.MustCompile(`(^|[^a-z0-9])qwen-?3`)

// thinkingOffByName returns the request fields that turn thinking off for a
// model known to reason by default, or nil when the name says nothing.
//
// Only the Qwen3 family is matched. Its chat template defines enable_thinking,
// and poisk asks both the expansion and the reranking model for a short answer
// under a small token budget, which a thinking model spends entirely on
// reasoning before returning empty content. Matching a family poisk cannot
// verify would send a field to a server that may reject the whole request.
func thinkingOffByName(model string) map[string]any {
	name := strings.ToLower(model)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if !qwen3Pattern.MatchString(name) {
		return nil
	}
	return map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}}
}

func NewClient(baseURL, apiKey, model string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}

	// An explicit extra_body key always wins, including one that turns thinking
	// back on.
	if _, set := c.extraBody["chat_template_kwargs"]; !set {
		c.autoExtraBody = thinkingOffByName(model)
	}
	c.sendAuto.Store(len(c.autoExtraBody) > 0)
	return c
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type completionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type completionMessage struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

type completionChoice struct {
	FinishReason string            `json:"finish_reason"`
	Message      completionMessage `json:"message"`
}

type completionResponse struct {
	Choices []completionChoice `json:"choices"`
}

// buildRequestBody marshals the request and overlays each extra map on top, in
// order. Extra fields win, so a caller can override max_tokens or temperature
// for a server that needs different values.
func buildRequestBody(req completionRequest, extras ...map[string]any) ([]byte, error) {
	base, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	total := 0
	for _, extra := range extras {
		total += len(extra)
	}
	if total == 0 {
		return base, nil
	}

	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, fmt.Errorf("merge extra_body: %w", err)
	}
	for _, extra := range extras {
		for k, v := range extra {
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("marshal extra_body key %q: %w", k, err)
			}
			merged[k] = raw
		}
	}
	return json.Marshal(merged)
}

// post sends one chat completion request and returns the decoded body, or the
// HTTP status when the server rejected it.
func (c *Client) post(ctx context.Context, jsonBody []byte) (*completionResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("completion request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("completion API %d: %s", resp.StatusCode, string(respBody))
	}

	var result completionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return &result, resp.StatusCode, nil
}

func (c *Client) Complete(ctx context.Context, messages []Message, temperature float64, maxTokens int) (string, error) {
	body := completionRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: &temperature,
		MaxTokens:   maxTokens,
	}

	auto := c.sendAuto.Load()
	extras := []map[string]any{c.extraBody}
	if auto {
		extras = []map[string]any{c.autoExtraBody, c.extraBody}
	}

	jsonBody, err := buildRequestBody(body, extras...)
	if err != nil {
		return "", err
	}

	result, status, err := c.post(ctx, jsonBody)
	if err != nil {
		// A 4xx while poisk added a field the config did not ask for is most
		// likely the server refusing that field. Drop it for good and retry, so
		// matching a model name can never break a server that rejects it.
		if !auto || status < 400 || status >= 500 {
			return "", err
		}
		slog.Warn("server rejected the automatic thinking-off field, retrying without it",
			"model", c.model, "status", status)
		c.sendAuto.Store(false)

		jsonBody, buildErr := buildRequestBody(body, c.extraBody)
		if buildErr != nil {
			return "", buildErr
		}
		result, _, err = c.post(ctx, jsonBody)
		if err != nil {
			return "", err
		}
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	choice := result.Choices[0]
	if strings.TrimSpace(choice.Message.Content) == "" {
		// A reasoning model can spend the whole max_tokens budget on thinking
		// and return no content at all. Callers fall back silently, so without
		// naming the cause here the only symptom is a slow search that ignores
		// its own reranking.
		if choice.Message.ReasoningContent != "" {
			return "", fmt.Errorf("empty completion (finish_reason=%q): the model returned only reasoning tokens; "+
				"turn thinking off with llm.extra_body, e.g. chat_template_kwargs = { enable_thinking = false }",
				choice.FinishReason)
		}
		return "", fmt.Errorf("empty completion (finish_reason=%q)", choice.FinishReason)
	}

	return choice.Message.Content, nil
}

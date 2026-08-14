package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

type Client struct {
	baseURL        string
	apiKey         string
	model          string
	dimensions     int
	sendDimensions bool
	matryoshka     bool
	http           *http.Client
}

func NewClient(baseURL, apiKey, model string, dimensions int, sendDimensions, matryoshka bool) *Client {
	return &Client{
		baseURL:        baseURL,
		apiKey:         apiKey,
		model:          model,
		dimensions:     dimensions,
		sendDimensions: sendDimensions,
		matryoshka:     matryoshka,
		http:           &http.Client{Timeout: 600 * time.Second},
	}
}

type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

const (
	maxRetries    = 3
	baseBackoff   = 500 * time.Millisecond
	backoffFactor = 2.0
)

func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body := embeddingRequest{
		Model: c.model,
		Input: texts,
	}
	if c.sendDimensions {
		body.Dimensions = c.dimensions
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			backoff := time.Duration(float64(baseBackoff) * math.Pow(backoffFactor, float64(attempt-1)))
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("embedding request: %w", context.Cause(ctx))
			case <-time.After(backoff):
			}
		}

		result, err := c.doEmbed(ctx, jsonBody)
		if err == nil {
			return c.parseResponse(result, len(texts))
		}
		lastErr = err

		if !isTransient(err, ctx) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) doEmbed(ctx context.Context, jsonBody []byte) (*embeddingResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &apiError{
			statusCode: resp.StatusCode,
			body:       string(respBody),
		}
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

func (c *Client) parseResponse(result *embeddingResponse, n int) ([][]float32, error) {
	embeddings := make([][]float32, n)
	for _, d := range result.Data {
		if d.Index >= len(embeddings) {
			return nil, fmt.Errorf("unexpected index %d in response", d.Index)
		}
		switch {
		case len(d.Embedding) == c.dimensions:
		case c.matryoshka && len(d.Embedding) > c.dimensions:
			d.Embedding = d.Embedding[:c.dimensions]
		default:
			return nil, fmt.Errorf("dimension mismatch: got %d, want %d", len(d.Embedding), c.dimensions)
		}
		normalize(d.Embedding)
		embeddings[d.Index] = d.Embedding
	}

	for i, e := range embeddings {
		if e == nil {
			return nil, fmt.Errorf("missing embedding for index %d", i)
		}
	}
	return embeddings, nil
}

type apiError struct {
	statusCode int
	body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("embedding API %d: %s", e.statusCode, e.body)
}

func isTransient(err error, ctx context.Context) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.statusCode >= 500 || ae.statusCode == http.StatusTooManyRequests
	}
	// Non-transient only if the caller's own context is done (cancellation or deadline).
	// HTTP client timeout wraps context.DeadlineExceeded via *url.Error, but that's
	// a transient network-level timeout — only bail if the caller signaled cancellation.
	if ctx.Err() != nil {
		return false
	}
	return true
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	batch, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return batch[0], nil
}

// normalize L2-normalizes v in place. Unit norm keeps every component within
// [-1, 1], which 'unit' int8 quantization requires (it does not clamp).
// Truncated Matryoshka prefixes are not unit-norm, so this must run after
// truncation. Zero vectors are left unchanged.
func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	norm := math.Sqrt(sum)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
}

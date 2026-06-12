package embed

import (
	"bytes"
	"context"
	"encoding/json"
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
		return nil, fmt.Errorf("embedding API %d: %s", resp.StatusCode, string(respBody))
	}

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	embeddings := make([][]float32, len(texts))
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

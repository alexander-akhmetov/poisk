package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL        string
	apiKey         string
	model          string
	dimensions     int
	sendDimensions bool
	http           *http.Client
}

func NewClient(baseURL, apiKey, model string, dimensions int, sendDimensions bool) *Client {
	return &Client{
		baseURL:        baseURL,
		apiKey:         apiKey,
		model:          model,
		dimensions:     dimensions,
		sendDimensions: sendDimensions,
		http:           &http.Client{Timeout: 120 * time.Second},
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

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/embeddings", bytes.NewReader(jsonBody))
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
		if len(d.Embedding) != c.dimensions {
			return nil, fmt.Errorf("dimension mismatch: got %d, want %d", len(d.Embedding), c.dimensions)
		}
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

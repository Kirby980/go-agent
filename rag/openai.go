package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/Kirby980/agent/internal/transport"
)

type openaiEmbedder struct {
	baseURL, apiKey, model string
	dim                    int
	client                 *transport.Client
}

func NewOpenAIEmbedder(baseURL, apiKey, model string, dim int) Embedder {
	return &openaiEmbedder{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		dim:     dim,
		client:  transport.NewClient(),
	}
}

func (e *openaiEmbedder) Dim() int {
	return e.dim
}

func (e *openaiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": e.model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.client.Do(req) // M01 的 Client.Do：自动重试 429/5xx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}

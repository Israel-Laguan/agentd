package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/gateway/spec"
)

func (o *OpenAI) embeddingsURL() string {
	return strings.TrimRight(o.cfg.BaseURL, "/") + "/embeddings"
}

var _ EmbedBackend = (*OpenAI)(nil)

// Embed implements EmbedBackend using the OpenAI embeddings API.
func (o *OpenAI) Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	if len(req.Input) == 0 {
		return spec.EmbedResponse{Vectors: nil}, nil
	}
	if o.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.cfg.Timeout)
		defer cancel()
	}
	model := req.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	data, _, err := postJSON(ctx, o.client, o.embeddingsURL(), openAIEmbedRequest{Model: model, Input: req.Input}, o.cfg.APIKey)
	if err != nil {
		return spec.EmbedResponse{}, err
	}
	var decoded openAIEmbedResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return spec.EmbedResponse{}, fmt.Errorf("decode openai embeddings: %w", err)
	}
	vectors := make([][]float32, len(req.Input))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(vectors) {
			continue
		}
		vectors[item.Index] = item.Embedding
	}
	modelUsed := decoded.Model
	if modelUsed == "" {
		modelUsed = model
	}
	return spec.EmbedResponse{Vectors: vectors, ProviderUsed: string(o.Name()), ModelUsed: modelUsed}, nil
}

type openAIEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbedResponse struct {
	Data  []openAIEmbedData `json:"data"`
	Model string            `json:"model"`
}

type openAIEmbedData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

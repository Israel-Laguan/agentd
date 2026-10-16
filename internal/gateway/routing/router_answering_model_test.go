package routing

import (
	"context"
	"testing"

	"agentd/internal/gateway/spec"
)

// answeringModelProvider answers as a different model than the one requested,
// like LiteLLM falling back from laguna-m.1 to laguna-s-2.1 (SP-021).
type answeringModelProvider struct {
	mockProvider
	answered string
	details  *spec.UsageDetails
}

func (p *answeringModelProvider) Generate(_ context.Context, _ spec.AIRequest) (spec.AIResponse, error) {
	return spec.AIResponse{Content: "ok", ProviderUsed: p.providerName, ModelUsed: p.answered, UsageDetails: p.details}, nil
}

// B-054: every successful response carries the model that answered in its
// usage details, so TOKEN_USAGE can record it.
func TestGenerateRecordsAnsweringModelInUsageDetails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		details    *spec.UsageDetails
		wantCached int
	}{
		{"no usage details from provider", nil, 0},
		{"cache details kept", &spec.UsageDetails{CachedTokens: 5}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &answeringModelProvider{mockProvider: mockProvider{providerName: "litellm", budget: 10000}, answered: "poolside/laguna-s-2.1", details: tt.details}
			resp, err := NewRouter(p).Generate(context.Background(), spec.AIRequest{Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}}})
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if resp.UsageDetails == nil || resp.UsageDetails.Model != "poolside/laguna-s-2.1" || resp.UsageDetails.CachedTokens != tt.wantCached {
				t.Fatalf("UsageDetails = %#v, want model poolside/laguna-s-2.1 and cached %d", resp.UsageDetails, tt.wantCached)
			}
		})
	}
}

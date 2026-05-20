package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestOpenAI_Embed_PreservesInputIndexAlignment(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(openAIEmbedResponse{
			Model: "text-embedding-3-small",
			Data: []openAIEmbedData{
				{Index: 0, Embedding: []float32{1, 0, 0}},
				{Index: 2, Embedding: []float32{0, 1, 0}},
			},
		})
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{BaseURL: srv.URL, APIKey: "test"}, srv.Client())
	resp, err := o.Embed(context.Background(), spec.EmbedRequest{
		Input: []string{"a", "b", "c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Vectors) != 3 {
		t.Fatalf("vectors len = %d, want 3", len(resp.Vectors))
	}
	if resp.Vectors[0] == nil || resp.Vectors[0][0] != 1 {
		t.Fatalf("vectors[0] = %v, want [1,0,0]", resp.Vectors[0])
	}
	if resp.Vectors[1] != nil {
		t.Fatalf("vectors[1] = %v, want nil", resp.Vectors[1])
	}
	if resp.Vectors[2] == nil || resp.Vectors[2][1] != 1 {
		t.Fatalf("vectors[2] = %v, want [0,1,0]", resp.Vectors[2])
	}
	if resp.ModelUsed != "text-embedding-3-small" {
		t.Fatalf("model used = %q", resp.ModelUsed)
	}
}

func TestOpenAI_Embed_DefaultModelIgnoresChatConfig(t *testing.T) {
	t.Parallel()
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body openAIEmbedRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		_ = json.NewEncoder(w).Encode(openAIEmbedResponse{
			Data: []openAIEmbedData{{Index: 0, Embedding: []float32{1}}},
		})
	}))
	defer srv.Close()

	o := NewOpenAI(spec.ProviderConfig{BaseURL: srv.URL, Model: "gpt-4", APIKey: "test"}, srv.Client())
	_, err := o.Embed(context.Background(), spec.EmbedRequest{Input: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotModel != "text-embedding-3-small" {
		t.Fatalf("model = %q, want text-embedding-3-small", gotModel)
	}
}

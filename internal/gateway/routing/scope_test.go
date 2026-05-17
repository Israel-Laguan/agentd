package routing

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

func TestAnalyzeScope_rejectsEmptyScopes(t *testing.T) {
	router := NewRouter(&jsonResponseProvider{
		providerName: "openai",
		content:      `{"single_scope":false,"confidence":0.9,"scopes":[],"reason":"multi"}`,
	})
	_, err := router.AnalyzeScope(context.Background(), "build a and b")
	if err == nil {
		t.Fatal("AnalyzeScope() error = nil, want ErrInvalidJSONResponse")
	}
	if !errors.Is(err, models.ErrInvalidJSONResponse) {
		t.Fatalf("AnalyzeScope() error = %v", err)
	}
}

func TestAnalyzeScope_singleScope(t *testing.T) {
	router := NewRouter(&jsonResponseProvider{
		providerName: "openai",
		content:      `{"single_scope":true,"confidence":0.9,"scopes":[{"id":"main","label":"Main app"}],"reason":"one"}`,
	})
	got, err := router.AnalyzeScope(context.Background(), "build one app")
	if err != nil {
		t.Fatalf("AnalyzeScope() error = %v", err)
	}
	if !got.SingleScope || len(got.Scopes) != 1 {
		t.Fatalf("analysis = %+v", got)
	}
}

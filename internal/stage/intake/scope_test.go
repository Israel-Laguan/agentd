package intake

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
	"agentd/internal/stage"
)

func TestAnalyzeScope_rejectsEmptyScopes(t *testing.T) {
	gw := &fakeGateway{replies: []string{`{"single_scope":false,"confidence":0.9,"scopes":[],"reason":"multi"}`}}
	_, err := AnalyzeScope(context.Background(), stage.NewModelAgent(gw), "build a and b")
	if err == nil {
		t.Fatal("AnalyzeScope() error = nil, want ErrInvalidJSONResponse")
	}
	if !errors.Is(err, models.ErrInvalidJSONResponse) {
		t.Fatalf("AnalyzeScope() error = %v", err)
	}
}

func TestAnalyzeScope_singleScope(t *testing.T) {
	gw := &fakeGateway{replies: []string{`{"single_scope":true,"confidence":0.9,"scopes":[{"id":"main","label":"Main app"}],"reason":"one"}`}}
	got, err := AnalyzeScope(context.Background(), stage.NewModelAgent(gw), "build one app")
	if err != nil {
		t.Fatalf("AnalyzeScope() error = %v", err)
	}
	if !got.SingleScope || len(got.Scopes) != 1 {
		t.Fatalf("analysis = %+v", got)
	}
}

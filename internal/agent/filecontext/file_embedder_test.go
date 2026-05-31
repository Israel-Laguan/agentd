package filecontext

import (
	"context"
	"testing"
)

func TestGatewayEmbedder_TypedNilReceiver(t *testing.T) {
	t.Parallel()
	var embedder Embedder = (*GatewayEmbedder)(nil)
	_, err := embedder.Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("expected error for typed-nil embedder")
	}
}

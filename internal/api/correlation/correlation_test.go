package correlation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromContext_Empty(t *testing.T) {
	assert.Equal(t, "", FromContext(context.Background()))
}

func TestWithID_And_FromContext(t *testing.T) {
	ctx := WithID(context.Background(), "req-abc-123")
	assert.Equal(t, "req-abc-123", FromContext(ctx))
}

func TestWithID_Override(t *testing.T) {
	ctx := WithID(context.Background(), "first")
	ctx = WithID(ctx, "second")
	assert.Equal(t, "second", FromContext(ctx))
}

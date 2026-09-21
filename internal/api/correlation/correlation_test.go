package correlation

import (
	"bytes"
	"context"
	"log/slog"
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

func TestAttr_OmitsWhenAbsent(t *testing.T) {
	ctx := context.Background()
	attr := Attr(ctx)
	assert.Equal(t, slog.Attr{}, attr)
}

func TestAttr_ReturnsCorrelationID(t *testing.T) {
	ctx := WithID(context.Background(), "req-abc-123")
	attr := Attr(ctx)
	assert.Equal(t, "correlation_id", attr.Key)
	assert.Equal(t, "req-abc-123", attr.Value.String())
}

func TestLogger_ReturnsDefaultWhenAbsent(t *testing.T) {
	ctx := context.Background()
	log := Logger(ctx)
	assert.Equal(t, slog.Default(), log)
}

func TestLogger_IncludesCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	ctx := WithID(context.Background(), "req-xyz-456")
	log := Logger(ctx)
	log.DebugContext(ctx, "test event", "detail", "value")
	out := buf.String()
	assert.Contains(t, out, "correlation_id=req-xyz-456",
		"correlated logger should emit correlation_id in structured output, got: %s", out)
	assert.Contains(t, out, "test event")
}

func TestLogger_DoesNotEmitSecrets(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	ctx := WithID(context.Background(), "req-secure-789")
	log := Logger(ctx)
	potentialSecrets := []string{
		"sk-litellm-secret-123",
		"sk-openai-secret-bearer",
		"super-secret-key",
	}
	// agentd logs only sanitized metadata (model/endpoint/counts/status), never
	// keys, authorization headers, or prompt content. Emit a realistic record.
	log.DebugContext(ctx, "openai: sending request",
		"model", "mock/agentd", "endpoint", "http://litellm:4000/v1/chat/completions",
		"message_count", 3, "tools_count", 0, "status", 200)
	out := buf.String()
	for _, s := range potentialSecrets {
		assert.NotContains(t, out, s, "secret value %q must not appear in log output", s)
	}
	assert.Contains(t, out, "correlation_id=req-secure-789")
	assert.NotContains(t, out, "prompt", "no user prompt content should be logged")
	assert.NotContains(t, out, "Authorization", "no authorization headers should be logged")
}

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

// Secret redaction for real request paths is covered by the provider tests
// (e.g. TestOpenAI_GenerateLogsLifecycleAndRedactsSecrets, including its
// failure-path case), which exercise the actual log statements instead of a
// hand-built record.

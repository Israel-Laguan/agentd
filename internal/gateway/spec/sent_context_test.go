package spec

import (
	"context"
	"testing"
)

// The callback only watches: changing the messages it is handed does not change
// what the provider is sent.
func TestSeeSentMessagesCannotChangeWhatIsSent(t *testing.T) {
	sent := []PromptMessage{{Role: "assistant", Content: "a", ToolCalls: []ToolCall{{ID: "1"}}}}
	ctx := WithSentMessages(context.Background(), func(m []PromptMessage) {
		m[0].Content = "changed"
		m[0].ToolCalls[0].ID = "changed"
	})

	SeeSentMessages(ctx, sent)

	if sent[0].Content != "a" || sent[0].ToolCalls[0].ID != "1" {
		t.Fatalf("sent = %+v, want it as it was before the callback", sent)
	}
}

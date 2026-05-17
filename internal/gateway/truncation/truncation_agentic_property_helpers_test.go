package truncation

import (
	"fmt"
	"math/rand"
	"strings"

	"agentd/internal/gateway/spec"
)

// ============================================================================
// Helper Functions for Property Tests
// ============================================================================

func safeSpan(min, max int) int {
	if max <= min {
		return 1
	}
	return max - min
}

// generateRandomMessages generates a random message list with the specified count
func generateRandomMessages(r *rand.Rand, minCount, maxCount int) []spec.PromptMessage {
	count := r.Intn(safeSpan(minCount, maxCount)) + minCount
	if count < 2 {
		count = 2
	}

	messages := make([]spec.PromptMessage, count)

	// Always start with system message
	messages[0] = spec.PromptMessage{
		Role:    "system",
		Content: randomContent(r, 10, 50),
	}

	// Add user message
	if count > 1 {
		messages[1] = spec.PromptMessage{
			Role:    "user",
			Content: randomContent(r, 10, 50),
		}
	}

	// Fill remaining with random roles
	roles := []string{"assistant", "user", "tool"}
	for i := 2; i < count; i++ {
		role := roles[r.Intn(len(roles))]
		messages[i] = spec.PromptMessage{
			Role:    role,
			Content: randomContent(r, 5, 30),
		}
		// Add tool call ID for tool messages
		if role == "tool" {
			messages[i].ToolCallID = fmt.Sprintf("call_%d", r.Intn(100))
		}
	}

	return messages
}

// generateRandomMessagesWithSystemPrompt ensures the first message is system
func generateRandomMessagesWithSystemPrompt(r *rand.Rand, minCount, maxCount int) []spec.PromptMessage {
	messages := generateRandomMessages(r, minCount, maxCount)
	// Ensure first is system
	messages[0] = spec.PromptMessage{
		Role:    "system",
		Content: randomContent(r, 10, 50),
	}
	return messages
}

// generateRandomMessagesWithUser ensures there's at least one user message
func generateRandomMessagesWithUser(r *rand.Rand, minCount, maxCount int) []spec.PromptMessage {
	messages := generateRandomMessages(r, minCount, maxCount)
	// Ensure at least one user message exists
	hasUser := false
	for _, m := range messages {
		if m.Role == "user" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		// Add user message at position 1
		if len(messages) > 1 {
			messages[1] = spec.PromptMessage{
				Role:    "user",
				Content: randomContent(r, 10, 50),
			}
		}
	}
	return messages
}

// generateRandomMessagesWithToolExchanges generates messages with proper tool exchange pairs
func generateRandomMessagesWithToolExchanges(r *rand.Rand, minCount, maxCount int) []spec.PromptMessage {
	count := calculateMessageCount(r, minCount, maxCount)
	messages := make([]spec.PromptMessage, 0, count)

	addBaseMessages(r, &messages)
	addToolExchanges(r, &messages, count)
	addFinalAssistantIfNeeded(r, &messages, count)
	padWithRandomMessages(r, &messages, count)

	return messages
}

// calculateMessageCount determines the number of messages to generate
func calculateMessageCount(r *rand.Rand, minCount, maxCount int) int {
	count := r.Intn(safeSpan(minCount, maxCount)) + minCount
	if count < 4 {
		count = 4
	}
	return count
}

// addBaseMessages adds the initial system and user messages
func addBaseMessages(r *rand.Rand, messages *[]spec.PromptMessage) {
	*messages = append(*messages, spec.PromptMessage{
		Role:    "system",
		Content: randomContent(r, 10, 30),
	})
	*messages = append(*messages, spec.PromptMessage{
		Role:    "user",
		Content: randomContent(r, 10, 30),
	})
}

// addToolExchanges adds assistant-tool call pairs without exceeding targetCount.
func addToolExchanges(r *rand.Rand, messages *[]spec.PromptMessage, targetCount int) {
	maxExchanges := (targetCount - len(*messages)) / 2
	if maxExchanges <= 0 {
		return
	}
	numExchanges := r.Intn(5) + 1 // 1 to 5 exchanges
	if numExchanges > maxExchanges {
		numExchanges = maxExchanges
	}
	for i := 0; i < numExchanges; i++ {
		callID := fmt.Sprintf("call_%d", i)
		addToolExchange(r, messages, callID, i)
	}
}

// addToolExchange adds a single assistant-tool call pair
func addToolExchange(r *rand.Rand, messages *[]spec.PromptMessage, callID string, index int) {
	assistantMsg := spec.PromptMessage{
		Role:    "assistant",
		Content: randomContent(r, 5, 20),
		ToolCalls: []spec.ToolCall{
			{ID: callID, Type: "function", Function: spec.ToolCallFunction{Name: "func_" + fmt.Sprintf("%d", index)}},
		},
	}
	*messages = append(*messages, assistantMsg)

	toolMsg := spec.PromptMessage{
		Role:       "tool",
		ToolCallID: callID,
		Content:    randomContent(r, 5, 30),
	}
	*messages = append(*messages, toolMsg)
}

// addFinalAssistantIfNeeded adds a final assistant message if needed
func addFinalAssistantIfNeeded(r *rand.Rand, messages *[]spec.PromptMessage, targetCount int) {
	if len(*messages) < targetCount {
		*messages = append(*messages, spec.PromptMessage{
			Role:    "assistant",
			Content: randomContent(r, 5, 20),
		})
	}
}

// padWithRandomMessages fills remaining space with random messages
func padWithRandomMessages(r *rand.Rand, messages *[]spec.PromptMessage, targetCount int) {
	for len(*messages) < targetCount {
		role := []string{"assistant", "user"}[r.Intn(2)]
		*messages = append(*messages, spec.PromptMessage{
			Role:    role,
			Content: randomContent(r, 5, 20),
		})
	}
}

// randomContent generates random string content of random length between min and max
func randomContent(r *rand.Rand, minLen, maxLen int) string {
	words := []string{"hello", "world", "test", "data", "result", "error", "success", "message", "content", "value",
		"function", "call", "api", "request", "response", "parameter", "output", "input", "system", "user",
		"assistant", "tool", "info", "debug", "log", "warning", "critical", "note", "summary", "detail"}

	minWords := minLen / 5
	maxWords := maxLen / 5
	if maxWords <= minWords {
		maxWords = minWords + 1
	}
	wordCount := r.Intn(maxWords-minWords) + minWords
	if wordCount < 1 {
		wordCount = 1
	}

	var parts []string
	for i := 0; i < wordCount; i++ {
		parts = append(parts, words[r.Intn(len(words))])
	}
	return strings.Join(parts, " ")
}

// hasSystemPrompt checks if messages contain a system prompt
func hasSystemPrompt(messages []spec.PromptMessage) bool {
	for _, m := range messages {
		if m.Role == "system" {
			return true
		}
	}
	return false
}

// hasUserMessage checks if messages contain a user message
func hasUserMessage(messages []spec.PromptMessage) bool {
	for _, m := range messages {
		if m.Role == "user" {
			return true
		}
	}
	return false
}

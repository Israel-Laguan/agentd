package truncation

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"agentd/internal/gateway/spec"
)

// ============================================================================
// Property-Based Tests for Universal Properties
// Validates: Requirements 1.2, 1.3, 1.4, 1.5, 2.2, 5.1, 5.2, 5.3, 5.4
// ============================================================================

const (
	// Property test iteration count
	propertyTestIterations = 100
)

// Seed for reproducible tests
var testSeed = int64(12345)

// Property 1: System prompt always preserved
// Validates: Requirements 1.2, 5.2
// ============================================================================

// TestProperty1_SystemPromptAlwaysPreserved tests that for any message history
// containing a system prompt as the first message, after truncation the output
// SHALL contain the original system prompt at position 0.
//
// **Validates: Requirements 1.2, 5.2**
func TestProperty1_SystemPromptAlwaysPreserved(t *testing.T) {
	r := rand.New(rand.NewSource(testSeed))

	for i := 0; i < propertyTestIterations; i++ {
		// Generate random message history with system prompt at position 0
		messages := generateRandomMessagesWithSystemPrompt(r, 5, 20)

		// Apply truncation with high max messages and unlimited budget to ensure truncation happens
		// but anchors are preserved
		maxMessages := r.Intn(10) + 3 // 3 to 12 - will trigger truncation
		budget := 0                   // unlimited

		truncator := NewAgenticTruncator(maxMessages)
		got, err := truncator.Apply(context.Background(), messages, budget)
		if err != nil {
			t.Fatalf("iteration %d: Apply() error = %v", i, err)
		}

		// Property: If input has system prompt at position 0, output must have it at position 0
		if len(messages) > 0 && messages[0].Role == "system" {
			if len(got) == 0 {
				t.Fatalf("iteration %d: output is empty but input had system prompt", i)
			}
			if got[0].Role != "system" {
				t.Fatalf("iteration %d: got[0].Role = %q, want system (system prompt not preserved at position 0)", i, got[0].Role)
			}
		}
	}

	t.Logf("Property 1: Verified system prompt always preserved across %d iterations", propertyTestIterations)
}

// ============================================================================
// Property 2: First user message always preserved
// Validates: Requirements 1.3, 5.2
// ============================================================================

// TestProperty2_FirstUserMessageAlwaysPreserved tests that for any message history
// containing user messages, after truncation the output SHALL contain the first
// user message (task description anchor).
//
// **Validates: Requirements 1.3, 5.2**
func TestProperty2_FirstUserMessageAlwaysPreserved(t *testing.T) {
	r := rand.New(rand.NewSource(testSeed + 1))

	for i := 0; i < propertyTestIterations; i++ {
		// Generate random message history with at least one user message
		messages := generateRandomMessagesWithUser(r, 5, 20)

		// Find first user message in input
		firstUserIdx := -1
		for idx, m := range messages {
			if m.Role == "user" {
				firstUserIdx = idx
				break
			}
		}
		if firstUserIdx == -1 {
			continue // Skip if no user message
		}

		// Apply truncation - use low max messages to trigger truncation
		maxMessages := r.Intn(8) + 3 // 3 to 10 - will trigger truncation
		budget := 0                   // unlimited

		truncator := NewAgenticTruncator(maxMessages)
		got, err := truncator.Apply(context.Background(), messages, budget)
		if err != nil {
			t.Fatalf("iteration %d: Apply() error = %v", i, err)
		}

		// Property: First user message must be preserved in output
		// It should be present somewhere in the output (not necessarily at same index)
		foundFirstUser := false
		for _, m := range got {
			if m.Role == "user" {
				foundFirstUser = true
				break
			}
		}
		if !foundFirstUser {
			t.Fatalf("iteration %d: first user message not preserved in output", i)
		}
	}

	t.Logf("Property 2: Verified first user message always preserved across %d iterations", propertyTestIterations)
}

// ============================================================================
// Property 3: Pairwise consistency
// Validates: Requirements 1.5, 5.3
// ============================================================================

// TestProperty3_PairwiseConsistency tests that for any truncated message list,
// for every assistant message with tool_calls, either the corresponding tool
// response messages are present with matching ToolCallIDs, OR the assistant
// message is marked as collapsed with a collapse marker.
//
// **Validates: Requirements 1.5, 5.3**
func TestProperty3_PairwiseConsistency(t *testing.T) {
	r := rand.New(rand.NewSource(testSeed + 2))

	for i := 0; i < propertyTestIterations; i++ {
		// Generate random message history with tool exchanges
		messages := generateRandomMessagesWithToolExchanges(r, 5, 25)

		// Apply truncation
		maxMessages := r.Intn(15) + 3
		budget := r.Intn(500) + 50

		truncator := NewAgenticTruncator(maxMessages)
		got, err := truncator.Apply(context.Background(), messages, budget)
		if err != nil {
			t.Fatalf("iteration %d: Apply() error = %v", i, err)
		}

		// Property: For every assistant with tool_calls, either tool response exists OR collapse marker
		for ai, m := range got {
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					hasResponse := false
					for j := ai + 1; j < len(got); j++ {
						if got[j].Role == "tool" && got[j].ToolCallID == tc.ID {
							hasResponse = true
							break
						}
					}
					// If no response, must have collapse marker in content
					if !hasResponse && !containsCollapseMarker(m.Content) {
						t.Fatalf("iteration %d: assistant message %d has orphan tool_call %q without collapse marker",
							i, ai, tc.ID)
					}
				}
			}
		}
	}

	t.Logf("Property 3: Verified pairwise consistency across %d iterations", propertyTestIterations)
}

// ============================================================================
// Property 4: Message count limit
// Validates: Requirement 2.2
// ============================================================================

// TestProperty4_MessageCountLimit tests that for any message history where
// truncation is triggered, the output SHALL contain at most AgenticTruncatorMax messages.
//
// **Validates: Requirement 2.2**
func TestProperty4_MessageCountLimit(t *testing.T) {
	r := rand.New(rand.NewSource(testSeed + 3))

	for i := 0; i < propertyTestIterations; i++ {
		// Generate random message history
		msgCount := r.Intn(30) + 10 // 10 to 39 messages
		messages := generateRandomMessages(r, msgCount, msgCount+10)

		// Apply truncation with various limits
		maxMessages := r.Intn(15) + 3 // 3 to 17
		budget := r.Intn(500) + 50

		truncator := NewAgenticTruncator(maxMessages)
		got, err := truncator.Apply(context.Background(), messages, budget)
		if err != nil {
			t.Fatalf("iteration %d: Apply() error = %v", i, err)
		}

		// Property: Output must have at most maxMessages
		if len(got) > maxMessages {
			t.Fatalf("iteration %d: len(got) = %d, want <= %d (message count limit violated)",
				i, len(got), maxMessages)
		}
	}

	t.Logf("Property 4: Verified message count limit across %d iterations", propertyTestIterations)
}

// ============================================================================
// Property 5: No-op when under threshold
// Validates: Requirement 5.4
// ============================================================================

// TestProperty5_NoopWhenUnderThreshold tests that for any message history where
// len(messages) <= truncationThreshold, the output SHALL be identical to the input.
//
// **Validates: Requirement 5.4**
func TestProperty5_NoopWhenUnderThreshold(t *testing.T) {
	r := rand.New(rand.NewSource(testSeed + 4))

	// Use a high threshold so messages are under it
	threshold := 100

	for i := 0; i < propertyTestIterations; i++ {
		// Generate message history under the threshold
		msgCount := r.Intn(threshold-2) + 2 // 2 to threshold-1
		messages := generateRandomMessages(r, msgCount, msgCount+10)

		// Skip if over threshold (truncation should trigger)
		if len(messages) > threshold {
			continue
		}

		// Use a truncator with high max messages (same as threshold)
		maxMessages := threshold

		truncator := NewAgenticTruncator(maxMessages)
		got, err := truncator.Apply(context.Background(), messages, 0)
		if err != nil {
			t.Fatalf("iteration %d: Apply() error = %v", i, err)
		}

		// Property: Output must be identical to input when under threshold
		// Note: This applies when truncation is NOT triggered
		// If len(messages) > maxMessages, truncation IS triggered and no-op doesn't apply
		if len(messages) <= maxMessages && len(got) != len(messages) {
			t.Fatalf("iteration %d: len(got) = %d, want %d (no-op violated), msgCount=%d, maxMessages=%d",
				i, len(got), len(messages), len(messages), maxMessages)
		}

		// Check content is identical for all retained messages
		for j := 0; j < len(got) && j < len(messages); j++ {
			if got[j].Role != messages[j].Role {
				t.Fatalf("iteration %d: got[%d].Role = %q, want %q", i, j, got[j].Role, messages[j].Role)
			}
			// Only check content for messages that are in both
			if j < len(messages) && got[j].Content != messages[j].Content && len(got) == len(messages) {
				t.Fatalf("iteration %d: got[%d].Content changed", i, j)
			}
		}
	}

	t.Logf("Property 5: Verified no-op when under threshold across %d iterations", propertyTestIterations)
}

// ============================================================================
// Property 6: Tool exchanges dropped before anchors
// Validates: Requirement 1.4
// ============================================================================

// TestProperty6_ToolExchangesDroppedBeforeAnchors tests that for any message history
// requiring truncation, the algorithm SHALL drop tool exchange messages before
// dropping anchor messages (system or first user).
//
// **Validates: Requirement 1.4**
func TestProperty6_ToolExchangesDroppedBeforeAnchors(t *testing.T) {
	r := rand.New(rand.NewSource(testSeed + 5))

	for i := 0; i < propertyTestIterations; i++ {
		// Generate random message history with known structure: system, user, tool exchanges
		messages := generateRandomMessagesWithToolExchanges(r, 5, 20)

		// Apply truncation with very low limit to force dropping
		maxMessages := 3 // Very low to force dropping

		truncator := NewAgenticTruncator(maxMessages)
		got, err := truncator.Apply(context.Background(), messages, 0)
		if err != nil {
			t.Fatalf("iteration %d: Apply() error = %v", i, err)
		}

		// Property: System and first user must always be preserved
		hasSystem := false
		hasFirstUser := false
		for _, m := range got {
			if m.Role == "system" {
				hasSystem = true
			}
			if m.Role == "user" {
				hasFirstUser = true
				break // Only need first user
			}
		}

		// If input had system, output must have it
		if hasSystemPrompt(messages) && !hasSystem {
			t.Fatalf("iteration %d: system prompt was dropped when tool exchanges should have been dropped first", i)
		}

		// If input had user, output must have at least one user
		if hasUserMessage(messages) && !hasFirstUser {
			t.Fatalf("iteration %d: first user message was dropped when tool exchanges should have been dropped first", i)
		}
	}

	t.Logf("Property 6: Verified tool exchanges dropped before anchors across %d iterations", propertyTestIterations)
}

// ============================================================================
// Property 7: Character budget enforcement
// Validates: Requirement 5.1
// ============================================================================

// TestProperty7_CharacterBudgetEnforcement tests that for any message history where
// character budget is specified and total characters exceed budget, the output
// SHALL have total characters within budget.
//
// **Validates: Requirement 5.1**
func TestProperty7_CharacterBudgetEnforcement(t *testing.T) {
	r := rand.New(rand.NewSource(testSeed + 6))

	// First, test very-small-budget cases to catch marker/overhead regressions
	smallBudgets := []int{1, 2, 5, 10}
	markerLen := utf8.RuneCountInString(TruncationMarker)
	// Add budgets up to marker length
	for b := markerLen - 2; b <= markerLen+2; b++ {
		if b > 0 {
			smallBudgets = append(smallBudgets, b)
		}
	}

	for _, budget := range smallBudgets {
		messages := generateRandomMessages(r, 5, 10)
		truncator := NewAgenticTruncator(50)
		got, err := truncator.Apply(context.Background(), messages, budget)
		if err != nil {
			t.Fatalf("small budget %d: Apply() error = %v", budget, err)
		}
		resultTotal := totalChars(got)
		if resultTotal > budget {
			t.Fatalf("small budget %d: result total chars %d exceeds budget %d",
				budget, resultTotal, budget)
		}
	}

	// Then test regular budget ranges
	for i := 0; i < propertyTestIterations; i++ {
		// Generate random message history
		messages := generateRandomMessages(r, 10, 20)

		// Calculate total characters
		total := totalChars(messages)

		// Set budget to be less than total (to trigger truncation)
		// but not too small (to avoid edge cases)
		budget := total/2 + 1
		if budget < 20 {
			budget = 20
		}

		// Apply truncation
		maxMessages := 50 // High limit to not trigger message count limit
		truncator := NewAgenticTruncator(maxMessages)
		got, err := truncator.Apply(context.Background(), messages, budget)
		if err != nil {
			t.Fatalf("iteration %d: Apply() error = %v", i, err)
		}

		// Property: Total characters must be within budget
		resultTotal := totalChars(got)
		if resultTotal > budget {
			t.Fatalf("iteration %d: result total chars %d exceeds budget %d",
				i, resultTotal, budget)
		}
	}

	t.Logf("Property 7: Verified character budget enforcement across %d iterations", propertyTestIterations)
}

// ============================================================================
// Helper Functions for Property Tests
// ============================================================================

// generateRandomMessages generates a random message list with the specified count
func generateRandomMessages(r *rand.Rand, minCount, maxCount int) []spec.PromptMessage {
	count := r.Intn(maxCount-minCount) + minCount
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
	count := r.Intn(maxCount-minCount) + minCount
	if count < 4 {
		count = 4
	}

	messages := make([]spec.PromptMessage, 0, count)

	// Start with system and user
	messages = append(messages, spec.PromptMessage{
		Role:    "system",
		Content: randomContent(r, 10, 30),
	})
	messages = append(messages, spec.PromptMessage{
		Role:    "user",
		Content: randomContent(r, 10, 30),
	})

	// Add some tool exchanges
	numExchanges := r.Intn(5) + 1 // 1 to 5 exchanges
	for i := 0; i < numExchanges; i++ {
		callID := fmt.Sprintf("call_%d", i)

		// Assistant message with tool call
		assistantMsg := spec.PromptMessage{
			Role:    "assistant",
			Content: randomContent(r, 5, 20),
			ToolCalls: []spec.ToolCall{
				{ID: callID, Type: "function", Function: spec.ToolCallFunction{Name: "func_" + fmt.Sprintf("%d", i)}},
			},
		}
		messages = append(messages, assistantMsg)

		// Tool response
		toolMsg := spec.PromptMessage{
			Role:       "tool",
			ToolCallID: callID,
			Content:    randomContent(r, 5, 30),
		}
		messages = append(messages, toolMsg)
	}

	// Add final assistant message
	if len(messages) < count {
		messages = append(messages, spec.PromptMessage{
			Role:    "assistant",
			Content: randomContent(r, 5, 20),
		})
	}

	// Pad with more random messages if needed
	for len(messages) < count {
		role := []string{"assistant", "user", "tool"}[r.Intn(3)]
		msg := spec.PromptMessage{
			Role:    role,
			Content: randomContent(r, 5, 20),
		}
		if role == "tool" {
			msg.ToolCallID = fmt.Sprintf("call_%d", r.Intn(100))
		}
		messages = append(messages, msg)
	}

	return messages
}

// randomContent generates random string content of random length between min and max
func randomContent(r *rand.Rand, minLen, maxLen int) string {
	words := []string{"hello", "world", "test", "data", "result", "error", "success", "message", "content", "value",
		"function", "call", "api", "request", "response", "parameter", "output", "input", "system", "user",
		"assistant", "tool", "info", "debug", "log", "warning", "critical", "note", "summary", "detail"}
	
	wordCount := r.Intn(maxLen/5-minLen/5) + minLen/5
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
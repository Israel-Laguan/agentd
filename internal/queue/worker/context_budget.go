package worker

import (
	"strings"
	"unicode/utf8"

	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

func (cm *ContextManager) enforceBudget(messages []spec.PromptMessage, totalBudget int) []spec.PromptMessage {
	if totalBudget <= 0 || totalChars(messages) <= totalBudget {
		return messages
	}

	// Find the point where working zone starts (after anchor and potential summary)
	anchor, rest := cm.partitionAnchor(messages)
	summaryIdx := summaryMessageIndex(rest)
	correctionEnd := correctionMessageEnd(rest)
	fixed, working := fixedAndWorkingMessages(anchor, rest, summaryIdx, correctionEnd)

	fixedChars := totalChars(fixed)
	remainingBudget := totalBudget - fixedChars
	if remainingBudget <= 0 {
		return fixed
	}

	return append(fixed, truncateWorkingMessages(working, remainingBudget)...)
}

func summaryMessageIndex(messages []spec.PromptMessage) int {
	for i, m := range messages {
		if m.Role == "system" && strings.HasPrefix(m.Content, "PREVIOUS CONTEXT SUMMARY") {
			return i
		}
	}
	return -1
}

func correctionMessageEnd(messages []spec.PromptMessage) int {
	for i, m := range messages {
		if m.Role != "system" || !IsCorrectionMessage(m.Content) {
			return i
		}
	}
	return len(messages)
}

func fixedAndWorkingMessages(
	anchor, rest []spec.PromptMessage,
	summaryIdx, correctionEnd int,
) ([]spec.PromptMessage, []spec.PromptMessage) {
	fixedCapacity := len(anchor) + correctionEnd
	if summaryIdx != -1 {
		fixedCapacity++
	}
	fixed := make([]spec.PromptMessage, 0, fixedCapacity)
	fixed = append(fixed, anchor...)
	fixed = append(fixed, rest[:correctionEnd]...)

	if summaryIdx == -1 {
		return fixed, rest[correctionEnd:]
	}
	if summaryIdx < correctionEnd {
		return fixed, rest[correctionEnd:]
	}

	fixed = append(fixed, rest[summaryIdx])
	working := append([]spec.PromptMessage{}, rest[correctionEnd:summaryIdx]...)
	working = append(working, rest[summaryIdx+1:]...)
	return fixed, working
}

func truncateWorkingMessages(working []spec.PromptMessage, budget int) []spec.PromptMessage {
	strategy := truncation.MiddleOutStrategy{}
	truncated := make([]spec.PromptMessage, 0, len(working))
	remaining := budget
	for _, m := range working {
		if remaining <= 0 {
			break
		}
		tm := m
		if utf8.RuneCountInString(tm.Content) > remaining {
			tm.Content = strategy.Truncate(tm.Content, remaining)
		}
		remaining -= utf8.RuneCountInString(tm.Content)
		if len(tm.ToolCalls) > 0 && remaining > 0 {
			tm.ToolCalls, remaining = truncateToolCalls(tm.ToolCalls, remaining, strategy)
		}
		truncated = append(truncated, tm)
	}
	return truncated
}

func truncateToolCalls(
	toolCalls []spec.ToolCall,
	remaining int,
	strategy truncation.MiddleOutStrategy,
) ([]spec.ToolCall, int) {
	tcCopy := make([]spec.ToolCall, len(toolCalls))
	copy(tcCopy, toolCalls)
	for j, tc := range tcCopy {
		remaining -= utf8.RuneCountInString(tc.Function.Name)
		if remaining <= 0 {
			tcCopy[j].Function.Arguments = ""
			return tcCopy[:j+1], remaining
		}
		if utf8.RuneCountInString(tc.Function.Arguments) > remaining {
			tcCopy[j].Function.Arguments = strategy.Truncate(tc.Function.Arguments, remaining)
		}
		remaining -= utf8.RuneCountInString(tcCopy[j].Function.Arguments)
	}
	return tcCopy, remaining
}

func mergeSummaries(a, b TurnSummary) TurnSummary {
	return TurnSummary{
		DecisionsMade:     append(a.DecisionsMade, b.DecisionsMade...),
		FactsEstablished:  append(a.FactsEstablished, b.FactsEstablished...),
		WorkCompleted:     append(a.WorkCompleted, b.WorkCompleted...),
		WorkRemaining:     append(a.WorkRemaining, b.WorkRemaining...),
		FilesModified:     append(a.FilesModified, b.FilesModified...),
		ErrorsEncountered: append(a.ErrorsEncountered, b.ErrorsEncountered...),
	}
}

// totalChars counts the total character count in all message contents.
func totalChars(messages []spec.PromptMessage) int {
	total := 0
	for _, m := range messages {
		total += utf8.RuneCountInString(m.Content)
		for _, tc := range m.ToolCalls {
			total += utf8.RuneCountInString(tc.Function.Name)
			total += utf8.RuneCountInString(tc.Function.Arguments)
		}
	}
	return total
}

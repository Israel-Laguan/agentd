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

	summaryIdx := -1
	for i, m := range rest {
		if m.Role == "system" && strings.HasPrefix(m.Content, "PREVIOUS CONTEXT SUMMARY") {
			summaryIdx = i
			break
		}
	}

	var fixed []spec.PromptMessage
	var working []spec.PromptMessage

	if summaryIdx != -1 {
		fixed = append(anchor, rest[:summaryIdx+1]...)
		working = rest[summaryIdx+1:]
	} else {
		fixed = anchor
		working = rest
	}

	fixedChars := totalChars(fixed)
	remainingBudget := totalBudget - fixedChars
	if remainingBudget <= 0 {
		return fixed
	}

	// Apply MiddleOut to messages in the working zone, tracking a global remaining counter.
	strategy := truncation.MiddleOutStrategy{}
	truncatedWorking := make([]spec.PromptMessage, 0, len(working))

	if len(working) > 0 {
		remaining := remainingBudget
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
				tcCopy := make([]spec.ToolCall, len(tm.ToolCalls))
				copy(tcCopy, tm.ToolCalls)
				for j, tc := range tcCopy {
					remaining -= utf8.RuneCountInString(tc.Function.Name)
					if remaining <= 0 {
						tcCopy[j].Function.Arguments = ""
						tcCopy = tcCopy[:j+1]
						break
					}
					if utf8.RuneCountInString(tc.Function.Arguments) > remaining {
						tcCopy[j].Function.Arguments = strategy.Truncate(tc.Function.Arguments, remaining)
					}
					remaining -= utf8.RuneCountInString(tcCopy[j].Function.Arguments)
				}
				tm.ToolCalls = tcCopy
			}
			truncatedWorking = append(truncatedWorking, tm)
		}
	}

	return append(fixed, truncatedWorking...)
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

package context

import (
	"strings"
	"unicode/utf8"

	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

func (cm *ContextManager) enforceBudget(messages []spec.PromptMessage, totalBudget int) []spec.PromptMessage {
	if totalBudget <= 0 || TotalChars(messages) <= totalBudget {
		return messages
	}

	// Find the point where working zone starts (after anchor and potential summary)
	anchor, rest := cm.PartitionAnchor(messages)
	summaryIdx := summaryMessageIndex(rest)
	correctionEnd := correctionMessageEnd(rest)
	fixed, working := fixedAndWorkingMessages(anchor, rest, summaryIdx, correctionEnd)

	fixedChars := TotalChars(fixed)
	remainingBudget := totalBudget - fixedChars
	if remainingBudget <= 0 {
		return fixed
	}

	return append(fixed, TruncateWorkingMessages(working, remainingBudget, cm.TruncationStrategy())...)
}

// SetTruncationStrategy sets how working messages over the budget are cut
// (gateway.truncation.strategy). Nil keeps middle-out.
func (cm *ContextManager) SetTruncationStrategy(strategy truncation.TruncationStrategy) {
	if cm == nil {
		return
	}
	cm.truncation = strategy
}

// TruncationStrategy returns the strategy that cuts working messages over the
// budget: the one set, else middle-out.
func (cm *ContextManager) TruncationStrategy() truncation.TruncationStrategy {
	if cm == nil || cm.truncation == nil {
		return truncation.MiddleOutStrategy{}
	}
	return cm.truncation
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

// TruncateWorkingMessages cuts a working message slice to budget with
// strategy; nil means middle-out. The newest messages are kept first, since
// the latest tool result is what the agent needs next, and an assistant tool
// call is cut together with its tool results, so none is left unpaired.
func TruncateWorkingMessages(working []spec.PromptMessage, budget int, strategy truncation.TruncationStrategy) []spec.PromptMessage {
	if strategy == nil {
		strategy = truncation.MiddleOutStrategy{}
	}
	units := workingUnits(working)
	var kept [][]spec.PromptMessage
	remaining := budget
	for i := len(units) - 1; i >= 0 && remaining > 0; i-- {
		var unit []spec.PromptMessage
		unit, remaining = fitUnit(units[i], remaining, strategy)
		kept = append(kept, unit)
	}
	out := make([]spec.PromptMessage, 0, len(working))
	for i := len(kept) - 1; i >= 0; i-- {
		out = append(out, kept[i]...)
	}
	return out
}

// workingUnits groups an assistant message that calls tools with the tool
// results after it; every other message is a unit of its own.
func workingUnits(working []spec.PromptMessage) [][]spec.PromptMessage {
	var units [][]spec.PromptMessage
	for _, m := range working {
		if last := len(units) - 1; m.Role == "tool" && last >= 0 && len(units[last][0].ToolCalls) > 0 {
			units[last] = append(units[last], m)
			continue
		}
		units = append(units, []spec.PromptMessage{m})
	}
	return units
}

// fitUnit cuts every message of a unit to what is left of the budget. No
// message is dropped: an emptied tool result still answers its call.
func fitUnit(unit []spec.PromptMessage, remaining int, strategy truncation.TruncationStrategy) ([]spec.PromptMessage, int) {
	out := make([]spec.PromptMessage, len(unit))
	for i, m := range unit {
		m.Content = cutTo(m.Content, remaining, strategy)
		remaining -= utf8.RuneCountInString(m.Content)
		if len(m.ToolCalls) > 0 {
			m.ToolCalls, remaining = truncateToolCalls(m.ToolCalls, remaining, strategy)
		}
		out[i] = m
	}
	return out, remaining
}

// truncateToolCalls cuts call arguments to the budget; every call is kept so
// its tool result stays paired.
func truncateToolCalls(
	toolCalls []spec.ToolCall,
	remaining int,
	strategy truncation.TruncationStrategy,
) ([]spec.ToolCall, int) {
	tcCopy := make([]spec.ToolCall, len(toolCalls))
	copy(tcCopy, toolCalls)
	for j := range tcCopy {
		remaining -= utf8.RuneCountInString(tcCopy[j].Function.Name)
		tcCopy[j].Function.Arguments = cutTo(tcCopy[j].Function.Arguments, remaining, strategy)
		remaining -= utf8.RuneCountInString(tcCopy[j].Function.Arguments)
	}
	return tcCopy, remaining
}

// cutTo fits s into budget characters: unchanged when it fits, empty when
// nothing is left.
func cutTo(s string, budget int, strategy truncation.TruncationStrategy) string {
	if budget <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) > budget {
		return strategy.Truncate(s, budget)
	}
	return s
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

// TotalChars counts message content and tool-call characters using the context
// manager's budget accounting.
func TotalChars(messages []spec.PromptMessage) int {
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

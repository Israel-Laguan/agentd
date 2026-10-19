package context

import (
	"strings"
	"unicode/utf8"

	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

// EnforceBudget fits messages into totalBudget by cutting the working zone.
// Anchor, correction, and summary messages are kept as fixed context so a cut
// never drops them in favor of older working turns.
func EnforceBudget(
	messages []spec.PromptMessage, totalBudget int, strategy truncation.TruncationStrategy,
) []spec.PromptMessage {
	if totalBudget <= 0 || TotalChars(messages) <= totalBudget {
		return messages
	}

	// Find the point where working zone starts (after anchor and potential summary)
	anchor, rest := PartitionAnchor(messages)
	summaryIdx := summaryMessageIndex(rest)
	correctionEnd := correctionMessageEnd(rest)
	fixed, working := fixedAndWorkingMessages(anchor, rest, summaryIdx, correctionEnd)

	fixedChars := TotalChars(fixed)
	remainingBudget := totalBudget - fixedChars
	if remainingBudget <= 0 {
		return fixed
	}

	return append(fixed, TruncateWorkingMessages(working, remainingBudget, strategy)...)
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

// fitUnit cuts every message of a unit to what is left of the budget. Call
// names cannot be cut, so a unit whose names do not fit is omitted whole,
// keeping every call paired with its result. Within a unit, the newest tool
// result is fitted first, since it is what the agent needs next; optional
// assistant content and call arguments get what remains.
func fitUnit(unit []spec.PromptMessage, remaining int, strategy truncation.TruncationStrategy) ([]spec.PromptMessage, int) {
	nameChars := toolCallNameChars(unit)
	if nameChars > remaining {
		return nil, remaining
	}
	remaining -= nameChars

	out := make([]spec.PromptMessage, len(unit))
	copy(out, unit)

	newestResult := -1
	if last := len(out) - 1; last > 0 && out[last].Role == "tool" {
		newestResult = last
		out[last].Content = cutTo(out[last].Content, remaining, strategy)
		remaining -= utf8.RuneCountInString(out[last].Content)
	}

	for i := range out {
		if i == newestResult {
			continue
		}
		out[i].Content = cutTo(out[i].Content, remaining, strategy)
		remaining -= utf8.RuneCountInString(out[i].Content)
		if len(out[i].ToolCalls) > 0 {
			out[i].ToolCalls, remaining = truncateToolCalls(out[i].ToolCalls, remaining, strategy)
		}
	}
	return out, remaining
}

func toolCallNameChars(unit []spec.PromptMessage) int {
	total := 0
	for _, m := range unit {
		for _, tc := range m.ToolCalls {
			total += utf8.RuneCountInString(tc.Function.Name)
		}
	}
	return total
}

// truncateToolCalls cuts call arguments to the budget; every call is kept so
// its tool result stays paired. Call names are unit overhead, already charged
// by fitUnit.
func truncateToolCalls(
	toolCalls []spec.ToolCall,
	remaining int,
	strategy truncation.TruncationStrategy,
) ([]spec.ToolCall, int) {
	tcCopy := make([]spec.ToolCall, len(toolCalls))
	copy(tcCopy, toolCalls)
	for j := range tcCopy {
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

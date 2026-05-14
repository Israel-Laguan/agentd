package worker

import (
	"strings"
	"time"

	"agentd/internal/gateway/spec"
)

const correctionPrefix = "[CORRECTION]"

// IsCorrectionMessage returns true if the message content starts with the correction prefix.
func IsCorrectionMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), correctionPrefix)
}

// InjectCorrection prepends a correction message to the working zone.
func (cm *ContextManager) InjectCorrection(rec CorrectionRecord) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}
	cm.corrections = append([]CorrectionRecord{rec}, cm.corrections...)
	correctionMsg := spec.PromptMessage{
		Role:    "system",
		Content: rec.FormatMessage(),
	}
	cm.workingZone.Messages = append(
		[]spec.PromptMessage{correctionMsg},
		cm.workingZone.Messages...,
	)
}

// InjectHumanCorrection is a convenience wrapper for manual corrections.
func (cm *ContextManager) InjectHumanCorrection(contradiction, correctFact string) {
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: contradiction,
		CorrectFact:   correctFact,
		Source:        CorrectionSourceHuman,
		Timestamp:     time.Now(),
	})
}

// AddSummary appends a compressed turn summary and indexes its facts.
func (cm *ContextManager) AddSummary(ts TurnSummary) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.summaries = append(cm.summaries, cloneTurnSummary(ts))
	if ts.Summary != "" {
		cm.compressedZone.Messages = append(cm.compressedZone.Messages, spec.PromptMessage{
			Role:    "system",
			Content: ts.Summary,
		})
	}
}

// AppendWorking adds a message to the tail of the working zone.
func (cm *ContextManager) AppendWorking(msg spec.PromptMessage) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.workingZone.Messages = append(cm.workingZone.Messages, msg)
}

// WorkingMessages returns a snapshot of the current working zone messages.
func (cm *ContextManager) WorkingMessages() []spec.PromptMessage {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	out := make([]spec.PromptMessage, len(cm.workingZone.Messages))
	copy(out, cm.workingZone.Messages)
	return out
}

// Corrections returns a snapshot of all injected corrections (newest first).
func (cm *ContextManager) Corrections() []CorrectionRecord {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	out := make([]CorrectionRecord, len(cm.corrections))
	copy(out, cm.corrections)
	return out
}

// Summaries returns a snapshot of all compressed turn summaries.
func (cm *ContextManager) Summaries() []TurnSummary {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	out := make([]TurnSummary, len(cm.summaries))
	for i := range cm.summaries {
		out[i] = cloneTurnSummary(cm.summaries[i])
	}
	return out
}

// Messages returns the full ordered message list: compressed zone followed by working zone.
func (cm *ContextManager) Messages() []spec.PromptMessage {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	total := len(cm.compressedZone.Messages) + len(cm.workingZone.Messages)
	out := make([]spec.PromptMessage, 0, total)
	out = append(out, cm.compressedZone.Messages...)
	out = append(out, cm.workingZone.Messages...)
	return out
}

// CheckToolResult examines a tool result for contradictions against facts
// in the compressed zone summaries.
func (cm *ContextManager) CheckToolResult(toolOutput string) []CorrectionRecord {
	cm.mu.Lock()
	summaries := make([]TurnSummary, len(cm.summaries))
	copy(summaries, cm.summaries)
	cm.mu.Unlock()
	detected := DetectContradictions(summaries, toolOutput)
	for _, rec := range detected {
		cm.InjectCorrection(rec)
	}
	return detected
}

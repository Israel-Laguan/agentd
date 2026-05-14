package worker

import (
	"fmt"
	"strings"
	"time"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
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

	for _, existing := range cm.corrections {
		if sameCorrection(existing, rec) {
			return
		}
	}
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

func sameCorrection(a, b CorrectionRecord) bool {
	return a.Contradiction == b.Contradiction &&
		a.CorrectFact == b.CorrectFact &&
		a.Source == b.Source
}

// MarkCommentCorrectionSeen records a task comment as processed for correction parsing.
func (cm *ContextManager) MarkCommentCorrectionSeen(c models.Comment) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.seenCorrections == nil {
		cm.seenCorrections = make(map[string]bool)
	}
	key := correctionCommentKey(c)
	if cm.seenCorrections[key] {
		return false
	}
	cm.seenCorrections[key] = true
	return true
}

func correctionCommentKey(c models.Comment) string {
	if strings.TrimSpace(c.ID) != "" {
		return "id:" + c.ID
	}
	return fmt.Sprintf("fallback:%s:%s:%s:%s",
		c.TaskID,
		c.Author,
		c.CreatedAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(c.Body),
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
	existing := append([]CorrectionRecord(nil), cm.corrections...)
	cm.mu.Unlock()

	detected := DetectContradictions(summaries, toolOutput)
	var unique []CorrectionRecord
	for _, rec := range detected {
		if hasCorrection(existing, rec) {
			continue
		}
		cm.InjectCorrection(rec)
		unique = append(unique, rec)
		existing = append(existing, rec)
	}
	return unique
}

func hasCorrection(records []CorrectionRecord, rec CorrectionRecord) bool {
	for _, existing := range records {
		if sameCorrection(existing, rec) {
			return true
		}
	}
	return false
}

// ParseCorrectionComment attempts to extract a CorrectionRecord from a
// human or reviewer comment. Expected format:
//
//	[CORRECT] was: <old fact>; is: <new fact>
//
// Returns nil if the comment doesn't match.
func ParseCorrectionComment(body string, source CorrectionSource) *CorrectionRecord {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "[CORRECT]") {
		return nil
	}
	content := strings.TrimPrefix(body, "[CORRECT]")
	parts := strings.Split(content, ";")
	var was, is string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "was:") {
			was = strings.TrimSpace(strings.TrimPrefix(p, "was:"))
		} else if strings.HasPrefix(p, "is:") {
			is = strings.TrimSpace(strings.TrimPrefix(p, "is:"))
		}
	}
	if was == "" || is == "" {
		return nil
	}
	return &CorrectionRecord{
		Contradiction: was,
		CorrectFact:   is,
		Source:        source,
		Timestamp:     time.Now(),
	}
}

// injectPendingCorrections inserts correction messages into the prepared
// context right after the anchor zone, replacing any previous injected copies.
func (cm *ContextManager) injectPendingCorrections(messages []spec.PromptMessage) []spec.PromptMessage {
	cm.mu.Lock()
	corrections := append([]CorrectionRecord(nil), cm.corrections...)
	cm.mu.Unlock()

	filtered := stripCorrectionMessages(messages)
	if len(corrections) == 0 {
		return filtered
	}

	anchor, rest := cm.partitionAnchor(filtered)
	corrMsgs := make([]spec.PromptMessage, 0, len(corrections))
	for _, c := range corrections {
		corrMsgs = append(corrMsgs, spec.PromptMessage{
			Role:    "system",
			Content: c.FormatMessage(),
		})
	}

	out := make([]spec.PromptMessage, 0, len(anchor)+len(corrMsgs)+len(rest))
	out = append(out, anchor...)
	out = append(out, corrMsgs...)
	out = append(out, rest...)
	return out
}

func stripCorrectionMessages(messages []spec.PromptMessage) []spec.PromptMessage {
	out := make([]spec.PromptMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" && IsCorrectionMessage(m.Content) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// CorrectionSnapshot returns all corrections accumulated during the session,
// suitable for submission to durable agent memory.
type CorrectionSnapshot struct {
	AgentID     string             `json:"agent_id"`
	TaskID      string             `json:"task_id"`
	Corrections []CorrectionRecord `json:"corrections"`
	CapturedAt  time.Time          `json:"captured_at"`
}

// SnapshotCorrections captures the current state of corrections for the session.
func (cm *ContextManager) SnapshotCorrections() CorrectionSnapshot {
	return CorrectionSnapshot{
		AgentID:     cm.agentID,
		TaskID:      cm.taskID,
		Corrections: cm.Corrections(),
		CapturedAt:  time.Now(),
	}
}

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

// InjectCorrection stores a correction record for later context injection.
// It returns false when an equivalent correction has already been stored.
func (cm *ContextManager) InjectCorrection(rec CorrectionRecord) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, existing := range cm.corrections {
		if sameCorrection(existing, rec) {
			return false
		}
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}
	cm.corrections = append([]CorrectionRecord{rec}, cm.corrections...)
	return true
}

func sameCorrection(a, b CorrectionRecord) bool {
	return a.Contradiction == b.Contradiction &&
		a.CorrectFact == b.CorrectFact
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
		return "id:" + c.ID + ":updated:" + c.UpdatedAt.UTC().Format(time.RFC3339Nano)
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
	_ = cm.InjectCorrection(CorrectionRecord{
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
		if cm.InjectCorrection(rec) {
			unique = append(unique, rec)
			existing = append(existing, rec)
		}
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
// The first "; is:" or ";is:" delimiter separates the old and new facts; if
// the old fact itself contains that delimiter, the format is ambiguous.
//
// Returns nil if the comment doesn't match.
func ParseCorrectionComment(body string, source CorrectionSource) *CorrectionRecord {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "[CORRECT]") {
		return nil
	}
	content := strings.TrimPrefix(body, "[CORRECT]")
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "was:") {
		return nil
	}
	content = strings.TrimSpace(strings.TrimPrefix(content, "was:"))
	delimiter := "; is:"
	idx := strings.Index(content, delimiter)
	if idx < 0 {
		delimiter = ";is:"
		idx = strings.Index(content, delimiter)
	}
	if idx < 0 {
		return nil
	}
	was := strings.TrimSpace(content[:idx])
	is := strings.TrimSpace(content[idx+len(delimiter):])
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

func (cm *ContextManager) CommentHighWater() time.Time {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.commentHighWater
}

func (cm *ContextManager) AdvanceCommentHighWater(comments []models.Comment) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, c := range comments {
		// ListCommentsSince uses a strict updated_at cursor, so advance with
		// UpdatedAt unless only CreatedAt is available.
		mark := c.UpdatedAt
		if mark.IsZero() {
			mark = c.CreatedAt
		}
		if mark.After(cm.commentHighWater) {
			cm.commentHighWater = mark
		}
	}
}

// ShouldPollComments atomically checks whether enough time has elapsed since
// the last comment poll and updates the last poll timestamp if so.
func (cm *ContextManager) ShouldPollComments(minInterval time.Duration) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	now := time.Now()
	if now.Sub(cm.lastCommentPoll) < minInterval {
		return false
	}
	cm.lastCommentPoll = now
	return true
}

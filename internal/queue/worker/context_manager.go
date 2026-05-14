package worker

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"agentd/internal/gateway/spec"
)

// CorrectionSource identifies the origin of a correction.
type CorrectionSource string

const (
	// CorrectionSourceTool indicates the correction was auto-detected from a
	// tool result that contradicts a fact in the compressed zone summary.
	CorrectionSourceTool CorrectionSource = "tool_result"

	// CorrectionSourceHuman indicates the correction was injected via the
	// system message API by a human operator.
	CorrectionSourceHuman CorrectionSource = "human"
)

// CorrectionRecord captures a single correction that overrides stale context.
type CorrectionRecord struct {
	Contradiction string           `json:"contradiction"`
	CorrectFact   string           `json:"correct_fact"`
	Source        CorrectionSource `json:"source"`
	Timestamp     time.Time        `json:"timestamp"`
}

// FormatMessage renders the correction as a prompt message suitable for
// prepending to the working zone.
func (cr CorrectionRecord) FormatMessage() string {
	return fmt.Sprintf(
		"[CORRECTION] Earlier context may state: %q. The correct information is: %q.",
		cr.Contradiction,
		cr.CorrectFact,
	)
}

// TurnSummary holds the compressed summary of a range of turns, including
// the key facts that were established during those turns.
type TurnSummary struct {
	Summary           string   `json:"summary"`
	FactsEstablished  []string `json:"facts_established"`
	TurnRangeStart    int      `json:"turn_range_start"`
	TurnRangeEnd      int      `json:"turn_range_end"`
}

// ContextZone represents one logical zone of the structured context window.
type ContextZone struct {
	Messages []spec.PromptMessage
}

// ContextManager manages structured context zones for the agentic loop.
// It maintains a compressed zone (summarized history) and a working zone
// (recent messages), with support for correction injection.
type ContextManager struct {
	mu              sync.Mutex
	compressedZone  ContextZone
	workingZone     ContextZone
	summaries       []TurnSummary
	corrections     []CorrectionRecord
}

func cloneTurnSummary(ts TurnSummary) TurnSummary {
	out := ts
	if ts.FactsEstablished != nil {
		out.FactsEstablished = append([]string(nil), ts.FactsEstablished...)
	}
	return out
}

// NewContextManager returns a ContextManager initialised with the given
// seed messages placed into the working zone.
func NewContextManager(seed []spec.PromptMessage) *ContextManager {
	return &ContextManager{
		workingZone: ContextZone{
			Messages: append([]spec.PromptMessage(nil), seed...),
		},
	}
}

// InjectCorrection prepends a correction message to the working zone. When
// multiple corrections are injected the newest correction appears first,
// giving it the highest positional authority.
func (cm *ContextManager) InjectCorrection(rec CorrectionRecord) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}

	// Store in reverse-chronological order (newest first).
	cm.corrections = append([]CorrectionRecord{rec}, cm.corrections...)

	correctionMsg := spec.PromptMessage{
		Role:    "system",
		Content: rec.FormatMessage(),
	}

	// Prepend to the working zone so the model sees corrections first.
	cm.workingZone.Messages = append(
		[]spec.PromptMessage{correctionMsg},
		cm.workingZone.Messages...,
	)
}

// InjectHumanCorrection is a convenience wrapper for manual (human-in-the-loop)
// corrections received via the system message API.
func (cm *ContextManager) InjectHumanCorrection(contradiction, correctFact string) {
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: contradiction,
		CorrectFact:   correctFact,
		Source:        CorrectionSourceHuman,
		Timestamp:     time.Now(),
	})
}

// AddSummary appends a compressed turn summary and indexes its facts for
// automatic contradiction detection.
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

// Messages returns the full ordered message list: compressed zone followed
// by working zone.
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
// in the compressed zone summaries. Any detected contradictions are
// automatically injected as corrections.
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

// correctionPrefix is the marker that identifies correction messages.
const correctionPrefix = "[CORRECTION]"

// IsCorrectionMessage returns true if the message content starts with the
// correction prefix marker.
func IsCorrectionMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), correctionPrefix)
}

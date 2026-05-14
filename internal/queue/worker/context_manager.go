package worker

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
)

// CorrectionSource identifies the origin of a correction.
type CorrectionSource string

const (
	CorrectionSourceTool     CorrectionSource = "tool_result"
	CorrectionSourceHuman    CorrectionSource = "human"
	CorrectionSourceReviewer CorrectionSource = "reviewer"
)

// CorrectionRecord captures a single correction that overrides stale context.
type CorrectionRecord struct {
	Contradiction string           `json:"contradiction"`
	CorrectFact   string           `json:"correct_fact"`
	Source        CorrectionSource `json:"source"`
	Timestamp     time.Time        `json:"timestamp"`
}

// FormatMessage renders the correction as a prompt message.
func (cr CorrectionRecord) FormatMessage() string {
	return fmt.Sprintf(
		"[CORRECTION] Earlier context may state: %q. The correct information is: %q.",
		cr.Contradiction, cr.CorrectFact,
	)
}

// TurnSummary represents a structured summary of one or more conversation turns.
type TurnSummary struct {
	Summary           string   `json:"summary,omitempty"`
	DecisionsMade     []string `json:"decisions_made"`
	FactsEstablished  []string `json:"facts_established"`
	WorkCompleted     []string `json:"work_completed"`
	WorkRemaining     []string `json:"work_remaining"`
	FilesModified     []string `json:"files_modified"`
	ErrorsEncountered []string `json:"errors_encountered"`
	TurnRangeStart    int      `json:"turn_range_start,omitempty"`
	TurnRangeEnd      int      `json:"turn_range_end,omitempty"`
}

// Turn represents a logical interaction cycle.
type Turn struct {
	Messages []spec.PromptMessage
}

// ContextZone represents one logical zone of the structured context window.
type ContextZone struct {
	Messages []spec.PromptMessage
}

// ContextManager handles partitioning of messages into context zones, applies
// compression/truncation strategies, and supports correction injection.
type ContextManager struct {
	cfg             config.AgenticContextConfig
	gateway         gateway.AIGateway
	summarizedTurns map[uint64]bool
	runningSummary  *TurnSummary
	cacheMu         sync.RWMutex
	agentID         string
	taskID          string
	mu              sync.Mutex
	compressedZone  ContextZone
	workingZone     ContextZone
	summaries       []TurnSummary
	corrections     []CorrectionRecord
}

func cloneTurnSummary(ts TurnSummary) TurnSummary {
	out := ts
	if ts.DecisionsMade != nil {
		out.DecisionsMade = append([]string(nil), ts.DecisionsMade...)
	}
	if ts.FactsEstablished != nil {
		out.FactsEstablished = append([]string(nil), ts.FactsEstablished...)
	}
	if ts.WorkCompleted != nil {
		out.WorkCompleted = append([]string(nil), ts.WorkCompleted...)
	}
	if ts.WorkRemaining != nil {
		out.WorkRemaining = append([]string(nil), ts.WorkRemaining...)
	}
	if ts.FilesModified != nil {
		out.FilesModified = append([]string(nil), ts.FilesModified...)
	}
	if ts.ErrorsEncountered != nil {
		out.ErrorsEncountered = append([]string(nil), ts.ErrorsEncountered...)
	}
	return out
}

// NewContextManager creates a new ContextManager with the given configuration.
// Negative config values are clamped to zero to prevent slice OOB panics.
func NewContextManager(cfg config.AgenticContextConfig, gw gateway.AIGateway, agentID, taskID string) *ContextManager {
	if cfg.RollingThresholdTurns < 0 {
		cfg.RollingThresholdTurns = 0
	}
	if cfg.KeepRecentTurns < 0 {
		cfg.KeepRecentTurns = 0
	}
	if cfg.AnchorBudget < 0 {
		cfg.AnchorBudget = 0
	}
	if cfg.WorkingBudget < 0 {
		cfg.WorkingBudget = 0
	}
	if cfg.CompressedBudget < 0 {
		cfg.CompressedBudget = 0
	}
	return &ContextManager{
		cfg:             cfg,
		gateway:         gw,
		summarizedTurns: make(map[uint64]bool),
		agentID:         agentID,
		taskID:          taskID,
	}
}

// PrepareContext partitions messages into zones and applies compression if needed.
func (cm *ContextManager) PrepareContext(ctx context.Context, messages []spec.PromptMessage) ([]spec.PromptMessage, error) {
	if len(messages) == 0 {
		return messages, nil
	}
	anchor, remaining := cm.partitionAnchor(messages)
	turns := cm.groupTurns(remaining)

	var out []spec.PromptMessage
	if len(turns) > cm.cfg.RollingThresholdTurns {
		var err error
		out, err = cm.applyRollingSummarization(ctx, anchor, turns)
		if err != nil {
			return nil, err
		}
	} else {
		out = cm.flatten(anchor, turns)
	}

	// Inject corrections after anchor, before compressed/working content
	out = cm.injectPendingCorrections(out)

	totalBudget := cm.cfg.AnchorBudget + cm.cfg.WorkingBudget + cm.cfg.CompressedBudget
	if totalBudget > 0 && totalChars(out) > totalBudget {
		out = cm.enforceBudget(out, totalBudget)
	}
	return out, nil
}

// injectPendingCorrections inserts correction messages into the prepared
// context right after the anchor zone (system prompt + first user message).
// This gives corrections positional authority over compressed zone summaries
// that may contain stale facts.
func (cm *ContextManager) injectPendingCorrections(messages []spec.PromptMessage) []spec.PromptMessage {
	cm.mu.Lock()
	corrections := append([]CorrectionRecord(nil), cm.corrections...)
	cm.mu.Unlock()

	if len(corrections) == 0 {
		return messages
	}

	// Find anchor boundary (end of system+first-user block)
	anchorEnd := 0
	foundUser := false
	for i, m := range messages {
		if m.Role == "system" {
			anchorEnd = i + 1
		} else if m.Role == "user" && !foundUser {
			anchorEnd = i + 1
			foundUser = true
		} else {
			break
		}
	}

	// Build correction messages (already newest-first in cm.corrections)
	corrMsgs := make([]spec.PromptMessage, 0, len(corrections))
	for _, c := range corrections {
		corrMsgs = append(corrMsgs, spec.PromptMessage{
			Role:    "system",
			Content: c.FormatMessage(),
		})
	}

	// Splice: anchor + corrections + rest
	out := make([]spec.PromptMessage, 0, len(messages)+len(corrMsgs))
	out = append(out, messages[:anchorEnd]...)
	out = append(out, corrMsgs...)
	out = append(out, messages[anchorEnd:]...)
	return out
}

func (cm *ContextManager) partitionAnchor(messages []spec.PromptMessage) ([]spec.PromptMessage, []spec.PromptMessage) {
	if len(messages) == 0 {
		return nil, nil
	}
	anchorEnd := 0
	for i, m := range messages {
		if m.Role == "system" {
			anchorEnd = i + 1
		} else {
			break
		}
	}
	for i := anchorEnd; i < len(messages); i++ {
		if messages[i].Role == "user" {
			anchorEnd = i + 1
			break
		}
	}
	return messages[:anchorEnd], messages[anchorEnd:]
}

func (cm *ContextManager) groupTurns(messages []spec.PromptMessage) []Turn {
	var turns []Turn
	var currentTurn Turn
	for _, m := range messages {
		if m.Role == "assistant" && len(currentTurn.Messages) > 0 {
			if cm.hasAssistant(currentTurn) {
				turns = append(turns, currentTurn)
				currentTurn = Turn{}
			}
		} else if m.Role == "user" && len(currentTurn.Messages) > 0 {
			turns = append(turns, currentTurn)
			currentTurn = Turn{}
		}
		currentTurn.Messages = append(currentTurn.Messages, m)
	}
	if len(currentTurn.Messages) > 0 {
		turns = append(turns, currentTurn)
	}
	return turns
}

func (cm *ContextManager) hasAssistant(t Turn) bool {
	for _, m := range t.Messages {
		if m.Role == "assistant" {
			return true
		}
	}
	return false
}

func (cm *ContextManager) applyRollingSummarization(ctx context.Context, anchor []spec.PromptMessage, turns []Turn) ([]spec.PromptMessage, error) {
	keepCount := cm.cfg.KeepRecentTurns
	if keepCount >= len(turns) {
		return cm.flatten(anchor, turns), nil
	}
	compressedTurns := turns[:len(turns)-keepCount]
	workingTurns := turns[len(turns)-keepCount:]

	summary, err := cm.summarizeTurns(ctx, compressedTurns)
	if err != nil {
		return nil, fmt.Errorf("summarize turns: %w", err)
	}
	summaryMsg := spec.PromptMessage{
		Role:    "system",
		Content: cm.formatSummary(summary),
	}
	out := append([]spec.PromptMessage{}, anchor...)
	out = append(out, summaryMsg)
	for _, t := range workingTurns {
		out = append(out, t.Messages...)
	}
	return out, nil
}

func (cm *ContextManager) summarizeTurns(ctx context.Context, turns []Turn) (TurnSummary, error) {
	var newTurns []Turn
	cm.cacheMu.RLock()
	for _, t := range turns {
		if !cm.summarizedTurns[cm.hashTurn(t)] {
			newTurns = append(newTurns, t)
		}
	}
	prev := cm.runningSummary
	cm.cacheMu.RUnlock()

	if len(newTurns) == 0 && prev != nil {
		return *prev, nil
	}
	s, err := cm.generateSummary(ctx, newTurns)
	if err != nil {
		return TurnSummary{}, err
	}
	if prev != nil {
		s = mergeSummaries(*prev, s)
	}
	cm.cacheMu.Lock()
	for _, t := range newTurns {
		cm.summarizedTurns[cm.hashTurn(t)] = true
	}
	cm.runningSummary = &s
	cm.cacheMu.Unlock()
	return s, nil
}

func (cm *ContextManager) hashTurn(t Turn) uint64 {
	h := fnv.New64a()
	for _, m := range t.Messages {
		h.Write([]byte{0x1F})
		h.Write([]byte(m.Role))
		h.Write([]byte{0x00})
		h.Write([]byte(m.Content))
		h.Write([]byte{0x00})
		h.Write([]byte{byte(len(m.ToolCalls))})
		for _, tc := range m.ToolCalls {
			h.Write([]byte(tc.ID))
			h.Write([]byte{0x00})
			h.Write([]byte(tc.Function.Name))
			h.Write([]byte{0x00})
			h.Write([]byte(tc.Function.Arguments))
			h.Write([]byte{0x00})
		}
		h.Write([]byte(m.ToolCallID))
	}
	return h.Sum64()
}

func (cm *ContextManager) flatten(anchor []spec.PromptMessage, turns []Turn) []spec.PromptMessage {
	out := append([]spec.PromptMessage{}, anchor...)
	for _, t := range turns {
		out = append(out, t.Messages...)
	}
	return out
}

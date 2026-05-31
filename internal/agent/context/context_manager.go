package context

import (
	"context"
	"fmt"
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
	cfg              config.AgenticContextConfig
	gateway          gateway.AIGateway
	summarizedTurns  map[uint64]bool
	runningSummary   *TurnSummary
	cacheMu          sync.RWMutex
	agentID          string
	taskID           string
	mu               sync.Mutex
	compressedZone   ContextZone
	workingZone      ContextZone
	summaries        []TurnSummary
	corrections      []CorrectionRecord
	seenCorrections  map[string]bool
	lastCommentPoll  time.Time
	commentHighWater time.Time
	goalTracker      *GoalTracker
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
		seenCorrections: make(map[string]bool),
		agentID:         agentID,
		taskID:          taskID,
	}
}

// TotalBudget returns the configured aggregate context character budget.
func (cm *ContextManager) TotalBudget() int {
	if cm == nil {
		return 0
	}
	return cm.cfg.AnchorBudget + cm.cfg.WorkingBudget + cm.cfg.CompressedBudget
}

// PrepareContext partitions messages into zones and applies compression if needed.
func (cm *ContextManager) PrepareContext(ctx context.Context, messages []spec.PromptMessage) ([]spec.PromptMessage, error) {
	return cm.prepareContext(ctx, messages, false)
}

// PrepareContextForceSummarize runs PrepareContext but forces rolling summarization
// even when turn count is below RollingThresholdTurns (context warning path).
func (cm *ContextManager) PrepareContextForceSummarize(ctx context.Context, messages []spec.PromptMessage) ([]spec.PromptMessage, error) {
	return cm.prepareContext(ctx, messages, true)
}

func (cm *ContextManager) prepareContext(ctx context.Context, messages []spec.PromptMessage, forceSummarize bool) ([]spec.PromptMessage, error) {
	if len(messages) == 0 {
		return messages, nil
	}
	messages = stripCorrectionMessages(messages)
	anchor, remaining := cm.partitionAnchor(messages)
	turns := cm.groupTurns(remaining)

	var out []spec.PromptMessage
	shouldSummarize := forceSummarize || len(turns) > cm.cfg.RollingThresholdTurns
	if shouldSummarize && len(turns) > 0 {
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

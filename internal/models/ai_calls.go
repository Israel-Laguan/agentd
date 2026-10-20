package models

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AICall is one persisted provider attempt (a gateway CallRecord). It holds
// sizes, counts, tokens and timing only, never prompt or response bodies.
type AICall struct {
	At               time.Time
	Kind             string
	Purpose          string
	Stage            string
	Role             string
	TaskID           string
	AgentID          string
	CorrelationID    string
	Provider         string
	Model            string
	Attempt          string
	Cascade          bool
	Outcome          string
	Truncated        bool
	MessageCount     int
	ToolCount        int
	InputChars       int
	OutputChars      int
	Tokens           int
	CachedTokens     int
	CacheWriteTokens int
	LatencyMS        int64
	// ContextMix is the input size by prompt section as JSON (chars and
	// message counts per section); empty when the call recorded none.
	ContextMix string
	// TruncatedBy names the layer that cut the input ("agentic_head",
	// "working_zone", "router"); empty when nothing was cut.
	TruncatedBy string
}

// AI usage report dimensions: the names accepted in AIUsageQuery.GroupBy.
const (
	AIDimPurpose = "purpose"
	AIDimStage   = "stage"
	AIDimModel   = "model"
	AIDimDay     = "day"
	AIDimTask    = "task"
	AIDimRequest = "request" // chat request: the correlation ID
)

// AIUsageDimensions lists every valid GroupBy name.
func AIUsageDimensions() []string {
	return []string{
		AIDimPurpose, AIDimStage, AIDimModel,
		AIDimDay, AIDimTask, AIDimRequest,
	}
}

// AIUsageQuery filters and groups the AI usage report. Zero filters match
// everything; From is inclusive and To exclusive.
type AIUsageQuery struct {
	From          time.Time
	To            time.Time
	TaskID        string
	CorrelationID string
	Purpose       string
	Stage         string
	Model         string
	GroupBy       []string
}

// ErrInvalidAIUsageQuery marks a report request the caller got wrong.
var ErrInvalidAIUsageQuery = errors.New("invalid ai usage query")

// Validate rejects an unknown GroupBy name or an inverted time range.
func (q AIUsageQuery) Validate() error {
	for _, g := range q.GroupBy {
		if !validAIDimension(g) {
			return fmt.Errorf("%w: unknown group_by %q (want one of %s)", ErrInvalidAIUsageQuery, g, strings.Join(AIUsageDimensions(), ", "))
		}
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.To.After(q.From) {
		return fmt.Errorf("%w: to must be after from", ErrInvalidAIUsageQuery)
	}
	return nil
}

func validAIDimension(name string) bool {
	for _, d := range AIUsageDimensions() {
		if d == name {
			return true
		}
	}
	return false
}

// AIUsageRow aggregates the attempts in one group. RepairRate is json_repair
// attempts over first attempts; CascadeRate is cascade attempts over all
// attempts. Latency is the average and the maximum, not percentiles: SQLite has
// no percentile aggregate and the table is not worth a second pass.
type AIUsageRow struct {
	Group          map[string]string `json:"group,omitempty"`
	Calls          int               `json:"calls"`
	Failed         int               `json:"failed"`
	Tokens         int               `json:"tokens"`
	CachedTokens   int               `json:"cached_tokens"`
	InputChars     int               `json:"input_chars"`
	OutputChars    int               `json:"output_chars"`
	AvgLatencyMS   float64           `json:"avg_latency_ms"`
	MaxLatencyMS   int64             `json:"max_latency_ms"`
	FirstAttempts  int               `json:"first_attempts"`
	RepairAttempts int               `json:"repair_attempts"`
	CascadeCalls   int               `json:"cascade_calls"`
	RepairRate     float64           `json:"repair_rate"`
	CascadeRate    float64           `json:"cascade_rate"`
	// MixChars sums input chars by prompt section (a key per section, zero
	// when absent); only calls recorded with a mix contribute.
	MixChars map[string]int `json:"mix_chars"`
	// TruncatedCalls counts calls whose input was cut; CutBy counts them per
	// layer (a call cut by two layers counts in both).
	TruncatedCalls int            `json:"truncated_calls"`
	CutBy          map[string]int `json:"cut_by"`
}

// AIUsageReport is the aggregated result. Dropped counts records the writer
// discarded since the daemon started; the service fills it, not the store.
type AIUsageReport struct {
	GroupBy []string     `json:"group_by"`
	Rows    []AIUsageRow `json:"rows"`
	Totals  AIUsageRow   `json:"totals"`
	Dropped uint64       `json:"dropped_records"`
}

// AICallStore persists and aggregates AI call records. Optional on a store, so
// callers type-assert for it the way they do for TokenCounter.
type AICallStore interface {
	InsertAICalls(ctx context.Context, calls []AICall) error
	AIUsageReport(ctx context.Context, q AIUsageQuery) (AIUsageReport, error)
}

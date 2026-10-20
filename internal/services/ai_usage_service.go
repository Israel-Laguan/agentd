package services

import (
	"context"
	"errors"

	"agentd/internal/models"
)

// ErrAIUsageUnavailable means this daemon has no store that records AI calls.
var ErrAIUsageUnavailable = errors.New("ai usage report is not available")

// AIUsageSource aggregates stored AI call records. The kanban store implements it.
type AIUsageSource interface {
	AIUsageReport(ctx context.Context, q models.AIUsageQuery) (models.AIUsageReport, error)
}

// ToolUseSource aggregates stored tool calls. The kanban store implements it.
type ToolUseSource interface {
	ToolUseReport(ctx context.Context, q models.ToolUseQuery) (models.ToolUseReport, error)
}

// ToolUse aggregates tool calls and repeated reads. It needs a source that
// also keeps tool calls; the kanban store does.
func (s *AIUsageService) ToolUse(ctx context.Context, q models.ToolUseQuery) (models.ToolUseReport, error) {
	if s == nil {
		return models.ToolUseReport{}, ErrAIUsageUnavailable
	}
	src, ok := s.source.(ToolUseSource)
	if !ok {
		return models.ToolUseReport{}, ErrAIUsageUnavailable
	}
	return src.ToolUseReport(ctx, q)
}

// DropCounter reports how many call records the writer discarded.
type DropCounter interface {
	Dropped() uint64
}

// AIUsageService answers "where do tokens go" from the stored call records.
type AIUsageService struct {
	source AIUsageSource
	drops  DropCounter
}

// NewAIUsageService builds the service. Both arguments may be nil: no source
// makes Report fail with ErrAIUsageUnavailable, no counter reports 0 dropped.
func NewAIUsageService(source AIUsageSource, drops DropCounter) *AIUsageService {
	return &AIUsageService{
		source: source,
		drops:  drops,
	}
}

// Report validates the query, groups by purpose when no dimension is given,
// and adds the writer's dropped count.
func (s *AIUsageService) Report(ctx context.Context, q models.AIUsageQuery) (models.AIUsageReport, error) {
	if s == nil || s.source == nil {
		return models.AIUsageReport{}, ErrAIUsageUnavailable
	}
	if len(q.GroupBy) == 0 {
		q.GroupBy = []string{models.AIDimPurpose}
	}
	if err := q.Validate(); err != nil {
		return models.AIUsageReport{}, err
	}
	rep, err := s.source.AIUsageReport(ctx, q)
	if err != nil {
		return models.AIUsageReport{}, err
	}
	if s.drops != nil {
		rep.Dropped = s.drops.Dropped()
	}
	return rep, nil
}

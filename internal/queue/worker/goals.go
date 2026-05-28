package worker

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// DefaultStallThreshold is the number of turns with < 10% progress
// before a goal is considered stalled. It is intentionally above
// config.DefaultMaxToolIterations (10) so a full tool-heavy burst is
// less likely to trigger a stall handoff before progress markers appear.
const DefaultStallThreshold = 25

// AgentGoal is a structured description of the desired end state that
// persists across all turns and against which progress can be measured.
type AgentGoal struct {
	Description       string   `json:"description"`
	SuccessCriteria   []string `json:"success_criteria"`
	Constraints       []string `json:"constraints"`
	CompletedCriteria []string `json:"completed_criteria"`
	BlockedCriteria   []string `json:"blocked_criteria"`
	TurnsActive       int      `json:"turns_active"`
}

// ProgressRatio returns the fraction of success criteria that have
// been completed. Returns 0 when there are no success criteria.
func (g *AgentGoal) ProgressRatio() float64 {
	if len(g.SuccessCriteria) == 0 {
		return 0
	}
	return float64(len(g.CompletedCriteria)) / float64(len(g.SuccessCriteria))
}

// IsStalled returns true when the goal has been active for more than
// the given threshold turns with less than 10% progress.
func (g *AgentGoal) IsStalled(threshold int) bool {
	return g.TurnsActive > threshold && g.ProgressRatio() < 0.1
}

// MarkCompleted adds criteria to the completed set, de-duplicating
// against already-completed entries.
func (g *AgentGoal) MarkCompleted(criteria []string) {
	allowed := g.successCriteriaSet()
	seen := make(map[string]struct{}, len(g.CompletedCriteria))
	for _, c := range g.CompletedCriteria {
		seen[c] = struct{}{}
	}
	var completed []string
	for _, c := range criteria {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := allowed[c]; !ok {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		g.CompletedCriteria = append(g.CompletedCriteria, c)
		completed = append(completed, c)
	}
	g.removeFromBlocked(completed)
}

// MarkBlocked adds criteria to the blocked set, de-duplicating and
// excluding any criteria that are already completed.
func (g *AgentGoal) MarkBlocked(criteria []string) {
	allowed := g.successCriteriaSet()
	completed := make(map[string]struct{}, len(g.CompletedCriteria))
	for _, c := range g.CompletedCriteria {
		completed[c] = struct{}{}
	}
	seen := make(map[string]struct{}, len(g.BlockedCriteria))
	for _, c := range g.BlockedCriteria {
		seen[c] = struct{}{}
	}
	for _, c := range criteria {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := allowed[c]; !ok {
			continue
		}
		if _, ok := completed[c]; ok {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		g.BlockedCriteria = append(g.BlockedCriteria, c)
	}
}

func (g *AgentGoal) successCriteriaSet() map[string]struct{} {
	allowed := make(map[string]struct{}, len(g.SuccessCriteria))
	for _, c := range g.SuccessCriteria {
		if v := strings.TrimSpace(c); v != "" {
			allowed[v] = struct{}{}
		}
	}
	return allowed
}

func (g *AgentGoal) removeFromBlocked(criteria []string) {
	remove := make(map[string]struct{}, len(criteria))
	for _, c := range criteria {
		remove[strings.TrimSpace(c)] = struct{}{}
	}
	filtered := g.BlockedCriteria[:0]
	for _, c := range g.BlockedCriteria {
		if _, ok := remove[c]; !ok {
			filtered = append(filtered, c)
		}
	}
	g.BlockedCriteria = filtered
}

// GoalTracker manages goal lifecycle within an agentic session.
type GoalTracker struct {
	mu             sync.Mutex
	goal           *AgentGoal
	stallThreshold int
	taskID         string
	projectID      string
	store          CriteriaUpdater
}

// CriteriaUpdater is the minimal store interface needed to persist
// completed success criteria. It is satisfied by *kanban.Store.
type CriteriaUpdater interface {
	UpdateCriteriaMet(ctx context.Context, id string, met []string) error
}

// GoalTrackerOption configures a GoalTracker.
type GoalTrackerOption func(*GoalTracker)

// WithStallThreshold overrides the default stall threshold.
func WithStallThreshold(threshold int) GoalTrackerOption {
	return func(gt *GoalTracker) {
		if threshold > 0 {
			gt.stallThreshold = threshold
		}
	}
}

// WithCriteriaStore wires a CriteriaUpdater so that AfterTurn persists
// CompletedCriteria to the database after each turn.
func WithCriteriaStore(s CriteriaUpdater) GoalTrackerOption {
	return func(gt *GoalTracker) {
		gt.store = s
	}
}

// NewGoalTracker creates a GoalTracker for the given task.
func NewGoalTracker(taskID, projectID string, opts ...GoalTrackerOption) *GoalTracker {
	gt := &GoalTracker{
		stallThreshold: DefaultStallThreshold,
		taskID:         taskID,
		projectID:      projectID,
	}
	for _, o := range opts {
		o(gt)
	}
	return gt
}

// SetGoal initialises the tracker with a goal. Calling SetGoal again
// replaces the existing goal.
func (gt *GoalTracker) SetGoal(goal AgentGoal) {
	gt.mu.Lock()
	defer gt.mu.Unlock()
	snap := goal
	snap.SuccessCriteria = append([]string(nil), goal.SuccessCriteria...)
	snap.Constraints = append([]string(nil), goal.Constraints...)
	snap.CompletedCriteria = append([]string(nil), goal.CompletedCriteria...)
	snap.BlockedCriteria = append([]string(nil), goal.BlockedCriteria...)
	gt.goal = &snap
}

// Goal returns a snapshot of the current goal, or nil if none is set.
func (gt *GoalTracker) Goal() *AgentGoal {
	gt.mu.Lock()
	defer gt.mu.Unlock()
	if gt.goal == nil {
		return nil
	}
	snap := *gt.goal
	snap.SuccessCriteria = append([]string(nil), gt.goal.SuccessCriteria...)
	snap.Constraints = append([]string(nil), gt.goal.Constraints...)
	snap.CompletedCriteria = append([]string(nil), gt.goal.CompletedCriteria...)
	snap.BlockedCriteria = append([]string(nil), gt.goal.BlockedCriteria...)
	return &snap
}

// AfterTurn is called after each agentic loop iteration. It increments
// TurnsActive, applies the model's reported progress, and checks for
// stall conditions. Returns true if the goal is stalled.
func (gt *GoalTracker) AfterTurn(ctx context.Context, completed, blocked []string) bool {
	gt.mu.Lock()
	if gt.goal == nil {
		gt.mu.Unlock()
		return false
	}

	gt.goal.TurnsActive++
	gt.goal.MarkCompleted(completed)
	gt.goal.MarkBlocked(blocked)
	stalled := gt.goal.IsStalled(gt.stallThreshold)
	met := append([]string(nil), gt.goal.CompletedCriteria...)
	store := gt.store
	taskID := gt.taskID
	gt.mu.Unlock()

	if store != nil && len(met) > 0 {
		if err := store.UpdateCriteriaMet(ctx, taskID, met); err != nil {
			slog.Warn("failed to persist criteria_met", "task_id", taskID, "error", err)
		}
	}

	return stalled
}

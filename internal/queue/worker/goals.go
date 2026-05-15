package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"agentd/internal/models"
)

// DefaultStallThreshold is the number of turns with < 10% progress
// before a goal is considered stalled.
const DefaultStallThreshold = 10

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
	seen := make(map[string]struct{}, len(g.CompletedCriteria))
	for _, c := range g.CompletedCriteria {
		seen[c] = struct{}{}
	}
	for _, c := range criteria {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		g.CompletedCriteria = append(g.CompletedCriteria, c)
	}
	g.removeFromBlocked(criteria)
}

// MarkBlocked adds criteria to the blocked set, de-duplicating and
// excluding any criteria that are already completed.
func (g *AgentGoal) MarkBlocked(criteria []string) {
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
	sink           models.EventSink
	taskID         string
	projectID      string
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

// NewGoalTracker creates a GoalTracker for the given task.
func NewGoalTracker(sink models.EventSink, taskID, projectID string, opts ...GoalTrackerOption) *GoalTracker {
	gt := &GoalTracker{
		stallThreshold: DefaultStallThreshold,
		sink:           sink,
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
	gt.goal = &goal
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
	defer gt.mu.Unlock()

	if gt.goal == nil {
		return false
	}

	gt.goal.TurnsActive++
	gt.goal.MarkCompleted(completed)
	gt.goal.MarkBlocked(blocked)

	if gt.goal.IsStalled(gt.stallThreshold) {
		gt.emitStalled(ctx)
		return true
	}
	return false
}

func (gt *GoalTracker) emitStalled(ctx context.Context) {
	if gt.sink == nil {
		return
	}
	payload := gt.stallPayload()
	slog.Warn("goal stalled",
		"task_id", gt.taskID,
		"turns_active", gt.goal.TurnsActive,
		"progress", gt.goal.ProgressRatio(),
	)
	_ = gt.sink.Emit(ctx, models.Event{
		ProjectID: gt.projectID,
		TaskID:    sql.NullString{String: gt.taskID, Valid: gt.taskID != ""},
		Type:      models.EventTypeGoalStalled,
		Payload:   payload,
	})
}

func (gt *GoalTracker) stallPayload() string {
	g := gt.goal
	return fmt.Sprintf(
		"goal stalled after %d turns (progress %.0f%%): %d/%d criteria met, %d blocked",
		g.TurnsActive,
		g.ProgressRatio()*100,
		len(g.CompletedCriteria),
		len(g.SuccessCriteria),
		len(g.BlockedCriteria),
	)
}

// parseGoalProgress extracts completed and blocked criteria markers from the
// model's response text. The model is expected to emit lines like:
//
//	[COMPLETED] criterion text
//	[BLOCKED] criterion text
func parseGoalProgress(content string) (completed, blocked []string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "[COMPLETED]"); ok {
			if v := strings.TrimSpace(after); v != "" {
				completed = append(completed, v)
			}
		} else if after, ok := strings.CutPrefix(line, "[BLOCKED]"); ok {
			if v := strings.TrimSpace(after); v != "" {
				blocked = append(blocked, v)
			}
		}
	}
	return completed, blocked
}

// GoalFromTask extracts a goal from a task's SuccessCriteria and
// description. Returns nil when no success criteria are present.
func GoalFromTask(task models.Task) *AgentGoal {
	if len(task.SuccessCriteria) == 0 {
		return nil
	}
	return &AgentGoal{
		Description:     task.Description,
		SuccessCriteria: append([]string(nil), task.SuccessCriteria...),
	}
}

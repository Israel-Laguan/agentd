package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

func (w *Worker) nextContextPackGeneration(ctx context.Context, originID string) (int, error) {
	children, err := w.store.ListChildTasksByRelation(ctx, originID, models.TaskRelationSpawnedBy)
	if err != nil {
		return 0, fmt.Errorf("list spawned children: %w", err)
	}
	count := 0
	for _, child := range children {
		if child.AgentID == tieredStepProfile[TieredStepContext] && strings.HasPrefix(child.Title, "context (re-gather") {
			count++
		}
	}
	return count + 1, nil
}

var regatherGenerationPattern = regexp.MustCompile(`^decision \(re-gather v([0-9]+)\):`)

func (w *Worker) handleNeedsContext(ctx context.Context, task models.Task, parentTask models.Task, reason string) error {
	current, err := w.store.GetTask(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("reload task before NEEDS_CONTEXT transition: %w", err)
	}
	if current.State != models.TaskStateNeedsContext {
		if _, err := w.store.UpdateTaskState(ctx, current.ID, current.UpdatedAt, models.TaskStateNeedsContext); err != nil {
			return fmt.Errorf("transition to NEEDS_CONTEXT: %w", err)
		}
	}
	w.Emit(ctx, task, "TIERED_NEEDS_CONTEXT", reason)

	generation, err := w.nextContextPackGeneration(ctx, parentTask.ID)
	if err != nil {
		return fmt.Errorf("determine pack generation: %w", err)
	}
	if match := regatherGenerationPattern.FindStringSubmatch(task.Title); len(match) == 2 {
		version, parseErr := strconv.Atoi(match[1])
		if parseErr == nil {
			generation = version + 1
		}
	}
	origin, err := w.store.GetTask(ctx, parentTask.ID)
	if err != nil {
		return fmt.Errorf("reload origin before re-gather: %w", err)
	}
	if origin.State == models.TaskStateFailed || origin.State == models.TaskStateFailedRequiresHuman {
		return fmt.Errorf("origin is %s; skipping re-gather", origin.State)
	}
	if generation > w.tieredCfg.EscalationConfig().MaxReGather {
		if _, err := w.store.UpdateTaskState(ctx, parentTask.ID, origin.UpdatedAt, models.TaskStateFailedRequiresHuman); err != nil {
			return fmt.Errorf("hand off after re-gather cap: %w", err)
		}
		w.Emit(ctx, parentTask, "TIERED_NEEDS_CONTEXT_CAP", fmt.Sprintf("max re-gathers reached: %d", generation-1))
		return nil
	}

	assignee := w.resolveAssignee(parentTask.Assignee)
	pair, err := w.spawnContextPackChain(ctx, parentTask, assignee, generation)
	if err != nil {
		return fmt.Errorf("spawn re-gather context/decision chain: %w", err)
	}
	rewired, err := w.store.RewireDependsOn(ctx, task.ID, pair.decisionID)
	if err != nil {
		return fmt.Errorf("rewire downstream dependents: %w", err)
	}
	w.Emit(ctx, task, "TIERED_PACK_REWIRED", fmt.Sprintf("generation=%d rewired=%d", generation, len(rewired)))
	return nil
}

func (w *Worker) resolveAssignee(a models.TaskAssignee) models.TaskAssignee {
	if !a.Valid() {
		return models.TaskAssigneeSystem
	}
	return a
}

type regatherPair struct {
	contextID  string
	decisionID string
}

func (w *Worker) spawnContextPackChain(ctx context.Context, parentTask models.Task, assignee models.TaskAssignee, generation int) (regatherPair, error) {
	now := time.Now().UTC()
	newContext := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   parentTask.ProjectID,
		AgentID:     tieredStepProfile[TieredStepContext],
		Title:       fmt.Sprintf("context (re-gather v%d): %s", generation, parentTask.Title),
		Description: parentTask.Description,
		State:       models.TaskStateReady,
		Assignee:    assignee,
	}
	newDecision := models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   parentTask.ProjectID,
		AgentID:     tieredStepProfile[TieredStepDecision],
		Title:       fmt.Sprintf("decision (re-gather v%d): %s", generation, parentTask.Title),
		Description: parentTask.Description,
		State:       models.TaskStatePending,
		Assignee:    assignee,
	}

	created, err := w.store.SpawnTieredContinuation(ctx, parentTask.ID, []models.TieredContinuationTask{
		{Task: newContext, IdempotencyKey: fmt.Sprintf("%s:regather:%d", parentTask.ID, generation)},
		{Task: newDecision, DependsOnID: newContext.ID},
	})
	if err != nil {
		return regatherPair{}, err
	}
	if len(created) != 2 {
		return regatherPair{}, fmt.Errorf("spawn re-gather returned %d tasks, want 2", len(created))
	}
	return regatherPair{contextID: created[0].ID, decisionID: created[1].ID}, nil
}

// parseContextPack attempts to extract a ContextPack from the LLM output.
// The output may contain JSON embedded in markdown code fences or plain text.
// Each fenced/plain candidate is decoded independently and must pass a
// structural pre-check (schema version, summary, paths) before it is
// accepted, so an earlier decodable-but-invalid payload cannot shadow a later
// valid one. The caller (parseAndConfigurePack) then stamps the authoritative
// task ID, normalizes budget counters, enforces the budget, and runs the full
// ContextPack.Validate before the pack is written.
func parseContextPack(output string) (*ContextPack, error) {
	for _, candidate := range extractJSONCandidates(output) {
		var cp ContextPack
		if err := json.Unmarshal([]byte(candidate), &cp); err != nil {
			continue
		}
		if !validContextPackCandidate(&cp) {
			continue
		}
		return &cp, nil
	}
	return nil, fmt.Errorf("no valid ContextPack JSON found in output")
}

// validContextPackCandidate reports whether cp carries the required ContextPack
// content independent of caller-stamped fields (TaskID) and caller-normalized
// budget counters (PathCount/CharCount), which parseAndConfigurePack sets after
// selection and before the full Validate.
func validContextPackCandidate(cp *ContextPack) bool {
	if cp.Version != ContextPackVersion {
		return false
	}
	if cp.Summary == "" || len(cp.Paths) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(cp.Paths))
	for _, p := range cp.Paths {
		if p == "" {
			return false
		}
		if _, dup := seen[p]; dup {
			return false
		}
		seen[p] = struct{}{}
	}
	for _, e := range cp.Excerpts {
		if e.Path == "" {
			return false
		}
	}
	return true
}

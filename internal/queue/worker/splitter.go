package worker

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

// TieredStepKind identifies one step of the tiered execution DAG produced
// by SplitIntoTieredDAG.
type TieredStepKind string

const (
	TieredStepContext  TieredStepKind = "context"
	TieredStepDecision TieredStepKind = "decision"
	TieredStepExecute  TieredStepKind = "execute"
	TieredStepVerify   TieredStepKind = "verify"
	// TieredStepEscalate is not part of the initial split; the escalation
	// ladder appends it after a verify conflict.
	TieredStepEscalate TieredStepKind = "escalate"
)

// tieredStepOrder is the fixed execution order of the M3 DAG: each step
// depends on the one before it.
var tieredStepOrder = []TieredStepKind{
	TieredStepContext,
	TieredStepDecision,
	TieredStepExecute,
	TieredStepVerify,
}

// tieredStepProfile maps each step kind to the agent profile that resolves
// its provider/model. Per-role gateway.role_models defaults still apply
// when a profile leaves provider/model empty; there is no separate
// tiered.models.<kind> override.
var tieredStepProfile = map[TieredStepKind]string{
	TieredStepContext:  "tier-context",
	TieredStepDecision: "tier-decision",
	TieredStepExecute:  "tier-execute",
	TieredStepVerify:   "tier-verify",
	TieredStepEscalate: "tier-escalate",
}

// SplitIntoTieredDAG builds the context -> decision -> execute -> verify
// child tasks for a parent task that has already passed ShouldRunTiered.
// It is a pure constructor: it does not check the gate itself, and callers
// are responsible for persisting the returned tasks and relations (and for
// leaving the parent's own one-shot path untouched when the gate fails).
//
// The context step starts READY since it has no dependency; the remaining
// steps start PENDING until their predecessor completes.
func SplitIntoTieredDAG(parent models.Task, now time.Time) ([]models.Task, []models.TaskRelation) {
	children := make([]models.Task, 0, len(tieredStepOrder))
	relations := make([]models.TaskRelation, 0, len(tieredStepOrder)*2-1)

	for i, kind := range tieredStepOrder {
		assignee := parent.Assignee
		if !assignee.Valid() {
			assignee = models.TaskAssigneeSystem
		}
		child := models.Task{
			BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
			ProjectID:   parent.ProjectID,
			AgentID:     tieredStepProfile[kind],
			Title:       fmt.Sprintf("%s: %s", kind, parent.Title),
			Description: parent.Description,
			State:       models.TaskStatePending,
			Assignee:    assignee,
		}
		if i == 0 {
			child.State = models.TaskStateReady
		} else {
			previous := children[i-1]
			child.DependsOn = []string{previous.ID}
			relations = append(relations, models.TaskRelation{
				ParentTaskID: previous.ID,
				ChildTaskID:  child.ID,
				RelationType: models.TaskRelationDependsOn,
			})
		}
		relations = append(relations, models.TaskRelation{
			ParentTaskID: parent.ID,
			ChildTaskID:  child.ID,
			RelationType: models.TaskRelationSpawnedBy,
		})
		children = append(children, child)
	}
	return children, relations
}

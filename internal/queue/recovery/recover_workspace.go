package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"agentd/internal/models"
)

// WorkspaceResetter empties a project's workspace. It is satisfied by
// sandbox.FSWorkspaceManager, which jails the reset under the workspace root.
type WorkspaceResetter interface {
	ResetProjectDir(ctx context.Context, projectID string) error
}

// BootOption tunes BootReconcile.
type BootOption func(*bootConfig)

type bootConfig struct {
	resetter WorkspaceResetter
}

func newBootConfig(opts []BootOption) bootConfig {
	var cfg bootConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithWorkspaceReset turns on recovery.clean_workspace_on_recover: before a
// recovered task is re-dispatched, its project's workspace is reset to the empty
// directory the project started as. Without it (the default) the re-run sees the
// interrupted attempt's files, see docs/architecture/recovery-rerun.md.
//
// Boot only: it runs before any worker starts, so nothing can be writing to the
// workspace. The stale-heartbeat sweep runs beside live dispatch and never resets.
func WithWorkspaceReset(r WorkspaceResetter) BootOption {
	return func(cfg *bootConfig) {
		cfg.resetter = r
	}
}

// resetRecoveredWorkspaces resets each affected project once and returns the
// tasks still READY. A task whose project cannot be reset safely is failed
// loudly (FAILED_REQUIRES_HUMAN plus a RECOVERY_RESET_REFUSED event) instead of
// being re-run on a workspace the operator asked to be clean.
func resetRecoveredWorkspaces(
	ctx context.Context,
	store models.KanbanStore,
	resetter WorkspaceResetter,
	sink models.EventSink,
	recovered []models.Task,
) []models.Task {
	recoveredIDs := make(map[string]struct{}, len(recovered))
	for _, task := range recovered {
		recoveredIDs[task.ID] = struct{}{}
	}
	outcome := make(map[string]error)
	resumed := make([]models.Task, 0, len(recovered))
	for _, task := range recovered {
		err, done := outcome[task.ProjectID]
		if !done {
			err = resetProject(ctx, store, resetter, task.ProjectID, recoveredIDs)
			outcome[task.ProjectID] = err
		}
		if err != nil {
			refuseReset(ctx, store, sink, task, err)
			continue
		}
		emitResetEvent(ctx, sink, models.EventTypeRecoveryWorkspaceReset, task, "workspace reset to its empty starting state before re-run")
		resumed = append(resumed, task)
	}
	return resumed
}

// resetProject refuses unless the reset provably restores the project's starting
// state without deleting output that belongs to another task.
func resetProject(
	ctx context.Context,
	store models.KanbanStore,
	resetter WorkspaceResetter,
	projectID string,
	recoveredIDs map[string]struct{},
) error {
	project, err := store.GetProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	if !project.StartedEmpty {
		return fmt.Errorf("project %s was not started empty, so its starting state is not recorded and cannot be restored", projectID)
	}
	tasks, err := store.ListTasksByProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list project tasks: %w", err)
	}
	for _, other := range tasks {
		if _, interrupted := recoveredIDs[other.ID]; interrupted {
			continue
		}
		if other.State == models.TaskStateCompleted || other.State == models.TaskStateRunning {
			return fmt.Errorf("task %s is %s and shares this workspace; a reset would delete its output", other.ID, other.State)
		}
	}
	if err := resetter.ResetProjectDir(ctx, projectID); err != nil {
		return fmt.Errorf("reset workspace: %w", err)
	}
	return nil
}

func refuseReset(ctx context.Context, store models.KanbanStore, sink models.EventSink, task models.Task, cause error) {
	slog.Error("recovery: workspace reset refused, failing recovered task",
		"task_id", task.ID, "project_id", task.ProjectID, "error", cause)
	current, err := store.GetTask(ctx, task.ID)
	if err == nil {
		_, err = store.UpdateTaskState(ctx, current.ID, current.UpdatedAt, models.TaskStateFailedRequiresHuman)
	}
	if err != nil {
		slog.Error("recovery: failing task after refused reset failed", "task_id", task.ID, "error", err)
	}
	emitResetEvent(ctx, sink, models.EventTypeRecoveryResetRefused, task,
		"recovery.clean_workspace_on_recover is on but the workspace was not reset: "+cause.Error())
}

func emitResetEvent(ctx context.Context, sink models.EventSink, eventType models.EventType, task models.Task, payload string) {
	if sink == nil {
		return
	}
	err := sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      eventType,
		Payload:   payload,
	})
	if err != nil {
		slog.Error("recovery: emit workspace reset event failed", "task_id", task.ID, "error", err)
	}
}

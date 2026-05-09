package kanban

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agentd/internal/models"
)

func (s *kanbanScenario) taskIsRunningWithPID(pidText string) error {
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		return fmt.Errorf("parse pid: %w", err)
	}
	task, err := s.seedTask("Ghost", models.TaskStateRunning, &pid)
	if err != nil {
		return err
	}
	s.lastTaskTitle = task.Title
	return nil
}

func (s *kanbanScenario) pidDoesNotExist(pidText string) error {
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		return fmt.Errorf("parse pid: %w", err)
	}
	s.alivePIDs = removePID(s.alivePIDs, pid)
	return nil
}

func (s *kanbanScenario) daemonRunsReconcileGhostTasks() error {
	var err error
	s.recovered, err = s.store.ReconcileGhostTasks(context.Background(), s.alivePIDs)
	s.lastErr = err
	return nil
}

func (s *kanbanScenario) taskStateShouldBeResetToReady() error {
	return s.taskShouldHaveState(s.lastTaskTitle, models.TaskStateReady)
}

func (s *kanbanScenario) recoveryEventShouldBeLogged() error {
	task, err := s.reloadTask(s.lastTaskTitle)
	if err != nil {
		return err
	}
	var count int
	err = s.store.db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM events WHERE task_id = ? AND type = 'RECOVERY'`,
		task.ID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("count recovery events: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("recovery events = %d, want 1", count)
	}
	return nil
}

func (s *kanbanScenario) projectExistsWithTasksAndEvents(taskCountText, eventCountText string) error {
	taskCount, err := strconv.Atoi(taskCountText)
	if err != nil {
		return fmt.Errorf("parse task count: %w", err)
	}
	eventCount, err := strconv.Atoi(eventCountText)
	if err != nil {
		return fmt.Errorf("parse event count: %w", err)
	}
	project, err := s.ensureProject()
	if err != nil {
		return err
	}
	taskIDs := make([]string, 0, taskCount)
	for i := 0; i < taskCount; i++ {
		task, err := s.seedTask(fmt.Sprintf("Cascade %02d", i), models.TaskStateReady, nil)
		if err != nil {
			return err
		}
		taskIDs = append(taskIDs, task.ID)
	}
	for i := 1; i < len(taskIDs); i++ {
		if err := insertTaskRelation(context.Background(), s.store.db, taskIDs[i-1], taskIDs[i]); err != nil {
			return err
		}
	}
	now := utcNow()
	for i := 0; i < eventCount; i++ {
		taskID := taskIDs[i%len(taskIDs)]
		if _, err := s.store.db.ExecContext(context.Background(), `
			INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("event-%02d", i), project.ID, taskID, "LOG", "event", formatTime(now), formatTime(now)); err != nil {
			return fmt.Errorf("insert cascade event: %w", err)
		}
	}
	return nil
}

func (s *kanbanScenario) deleteProjectFromProjectsTable() error {
	project, err := s.ensureProject()
	if err != nil {
		return err
	}
	_, err = s.store.db.ExecContext(context.Background(), `DELETE FROM projects WHERE id = ?`, project.ID)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (s *kanbanScenario) tasksTableShouldBeEmpty() error {
	return s.tableCountShouldBe("tasks", 0)
}

func (s *kanbanScenario) eventsTableShouldBeEmpty() error {
	return s.tableCountShouldBe("events", 0)
}

func (s *kanbanScenario) relationsTableShouldBeEmpty() error {
	return s.tableCountShouldBe("task_relations", 0)
}

func (s *kanbanScenario) refreshProjectTasks() error {
	if s.project == nil {
		return nil
	}
	tasks, err := s.store.ListTasksByProject(context.Background(), s.project.ID)
	if err != nil {
		return err
	}
	s.tasks = make(map[string]models.Task, len(tasks))
	for _, task := range tasks {
		s.tasks[task.Title] = task
	}
	return nil
}

func (s *kanbanScenario) reloadTask(title string) (models.Task, error) {
	title = normalizeTaskTitle(title)
	task, ok := s.tasks[title]
	if !ok {
		return models.Task{}, fmt.Errorf("task %q not found", title)
	}
	fresh, err := s.store.GetTask(context.Background(), task.ID)
	if err != nil {
		return models.Task{}, err
	}
	s.tasks[fresh.Title] = *fresh
	return *fresh, nil
}

func (s *kanbanScenario) ensureProject() (*models.Project, error) {
	if s.project != nil {
		return s.project, nil
	}
	now := utcNow()
	project := &models.Project{
		BaseEntity:    models.BaseEntity{ID: "project", CreatedAt: now, UpdatedAt: now},
		Name:          "acceptance project",
		OriginalInput: "acceptance criteria",
		WorkspacePath: "acceptance-project",
		Status:        "ACTIVE",
	}
	_, err := s.store.db.ExecContext(context.Background(), `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		project.ID, project.Name, project.OriginalInput, project.WorkspacePath, project.Status,
		formatTime(project.CreatedAt), formatTime(project.UpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("insert acceptance project: %w", err)
	}
	s.project = project
	return s.project, nil
}

func (s *kanbanScenario) seedTask(title string, state models.TaskState, osPID *int) (models.Task, error) {
	title = normalizeTaskTitle(title)
	project, err := s.ensureProject()
	if err != nil {
		return models.Task{}, err
	}
	now := utcNow()
	task := models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-" + sanitizeTitle(title), CreatedAt: now, UpdatedAt: now},
		ProjectID:   project.ID,
		AgentID:     defaultAgentID,
		Title:       title,
		Description: "acceptance task",
		State:       state,
		Assignee:    models.TaskAssigneeSystem,
		OSProcessID: osPID,
	}
	_, err = s.store.db.ExecContext(context.Background(), `
		INSERT INTO tasks (
			id, project_id, agent_id, title, description, state, assignee,
			os_process_id, started_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.ProjectID, task.AgentID, task.Title, task.Description, task.State, task.Assignee,
		nullableProcessID(task.OSProcessID), nil, nil, task.RetryCount, task.TokenUsage,
		formatTime(task.CreatedAt), formatTime(task.UpdatedAt))
	if err != nil {
		return models.Task{}, fmt.Errorf("insert acceptance task %q: %w", title, err)
	}
	s.tasks[task.Title] = task
	return task, nil
}

func (s *kanbanScenario) tableCountShouldBe(table string, want int) error {
	var got int
	if err := s.store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
		return fmt.Errorf("count %s: %w", table, err)
	}
	if got != want {
		return fmt.Errorf("%s count = %d, want %d", table, got, want)
	}
	return nil
}

func parseTaskTitles(text string) []string {
	if idx := strings.Index(text, ":"); idx >= 0 {
		text = text[idx+1:]
	}
	normalized := strings.ReplaceAll(text, " and ", ", ")
	parts := strings.Split(normalized, ",")
	titles := make([]string, 0, len(parts))
	for _, part := range parts {
		title := normalizeTaskTitle(part)
		if title != "" {
			titles = append(titles, title)
		}
	}
	return titles
}

func normalizeTaskTitle(title string) string {
	title = strings.TrimSpace(title)
	title = strings.Trim(title, `"`)
	title = strings.TrimPrefix(title, "Task ")
	return strings.TrimSpace(title)
}

func sanitizeTitle(title string) string {
	replacer := strings.NewReplacer(" ", "-", "_", "-", `"`, "")
	return strings.ToLower(replacer.Replace(title))
}

func nullableProcessID(pid *int) any {
	if pid == nil {
		return nil
	}
	return *pid
}

func removePID(pids []int, removed int) []int {
	kept := pids[:0]
	for _, pid := range pids {
		if pid != removed {
			kept = append(kept, pid)
		}
	}
	return kept
}

package kanban

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *Store) GetAgentProfile(ctx context.Context, id string) (*models.AgentProfile, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, provider, model, temperature, system_prompt, role, max_tokens, updated_at
		FROM agent_profiles WHERE id = ?`, id)
	profile, err := scanAgentProfile(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("agent profile %s: %w", id, models.ErrAgentProfileNotFound)
		}
		return nil, err
	}
	return profile, nil
}

func (s *Store) ListAgentProfiles(ctx context.Context) ([]models.AgentProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, provider, model, temperature, system_prompt, role, max_tokens, updated_at
		FROM agent_profiles ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	defer closeRows(rows)

	var out []models.AgentProfile
	for rows.Next() {
		profile, err := scanAgentProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent profiles: %w", err)
	}
	return out, nil
}

func (s *Store) UpsertAgentProfile(ctx context.Context, p models.AgentProfile) error {
	now := utcNow()
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	if p.Role == "" {
		p.Role = "CODE_GEN"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_profiles (id, name, provider, model, temperature, system_prompt, role, max_tokens, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			provider = excluded.provider,
			model = excluded.model,
			temperature = excluded.temperature,
			system_prompt = excluded.system_prompt,
			role = excluded.role,
			max_tokens = excluded.max_tokens,
			updated_at = excluded.updated_at`,
		p.ID, p.Name, p.Provider, p.Model, p.Temperature, nullString(p.SystemPrompt),
		p.Role, p.MaxTokens, formatTime(p.UpdatedAt))
	if err != nil {
		return fmt.Errorf("upsert agent profile: %w", err)
	}
	return nil
}

// DeleteAgentProfile removes a profile. It refuses to delete the built-in
// "default" profile and refuses if any task still references the id.
func (s *Store) DeleteAgentProfile(ctx context.Context, id string) error {
	if id == defaultAgentID {
		return models.ErrAgentProfileProtected
	}
	var inUse int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM tasks WHERE agent_id = ? LIMIT 1`, id).Scan(&inUse)
	if err == nil {
		return models.ErrAgentProfileInUse
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check agent profile usage: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM agent_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete agent profile: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete agent profile rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("agent profile %s: %w", id, models.ErrAgentProfileNotFound)
	}
	return nil
}

// AssignTaskAgent updates tasks.agent_id with optimistic locking. It refuses
// to retarget a task that is currently RUNNING; the caller should pause the
// task via AddCommentAndPause first if a live swap is intended.
func (s *Store) AssignTaskAgent(
	ctx context.Context,
	taskID string,
	expectedUpdatedAt time.Time,
	agentID string,
) (*models.Task, error) {
	if _, err := s.GetAgentProfile(ctx, agentID); err != nil {
		return nil, err
	}
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		now := utcNow()
		result, err := s.db.ExecContext(ctx, `
			UPDATE tasks
			SET agent_id = ?, updated_at = ?
			WHERE id = ? AND updated_at = ? AND state != ?`,
			agentID, formatTime(now), taskID, formatTime(expectedUpdatedAt),
			models.TaskStateRunning)
		if err != nil {
			return nil, fmt.Errorf("assign task agent: %w", err)
		}
		if err := requireRowsAffected(result, 1, models.ErrStateConflict); err != nil {
			return nil, err
		}
		return s.GetTask(ctx, taskID)
	})
}

func scanAgentProfile(row scanner) (*models.AgentProfile, error) {
	var p models.AgentProfile
	var updatedAt string
	err := row.Scan(&p.ID, &p.Name, &p.Provider, &p.Model, &p.Temperature,
		&p.SystemPrompt, &p.Role, &p.MaxTokens, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan agent profile: %w", err)
	}
	updated, err := parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt = updated
	return &p, nil
}
